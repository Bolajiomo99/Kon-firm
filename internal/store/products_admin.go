package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrSKUTaken is returned when a SKU already exists.
var ErrSKUTaken = errors.New("store: that SKU already exists")

// ErrInUse is returned when a product cannot be hard-deleted.
var ErrInUse = errors.New("store: product has orders against it")

// ProductInput is an admin-supplied product.
type ProductInput struct {
	SKU           string
	Barcode       string
	Name          string
	Description   string
	PriceKobo     int64
	CompareAtKobo *int64
	Stock         int
	ImageURL      string
	Category      string
	IsNew         bool
	Active        bool
}

func (in *ProductInput) normalise() error {
	in.SKU = strings.ToUpper(strings.TrimSpace(in.SKU))
	in.Name = strings.TrimSpace(in.Name)
	in.Category = strings.TrimSpace(in.Category)
	in.Barcode = strings.TrimSpace(in.Barcode)
	in.ImageURL = strings.TrimSpace(in.ImageURL)

	switch {
	case in.SKU == "":
		return fmt.Errorf("store: SKU is required")
	case in.Name == "":
		return fmt.Errorf("store: name is required")
	case in.PriceKobo <= 0:
		return fmt.Errorf("store: price must be greater than zero")
	case in.Stock < 0:
		return fmt.Errorf("store: stock cannot be negative")
	}
	if in.Category == "" {
		in.Category = "Other"
	}
	// A "was" price that is not higher than the real one is not a discount;
	// showing it would advertise a saving that does not exist.
	if in.CompareAtKobo != nil && *in.CompareAtKobo <= in.PriceKobo {
		return fmt.Errorf("store: the compare-at price must be higher than the price")
	}
	return nil
}

// CreateProduct adds a product.
func (s *Store) CreateProduct(ctx context.Context, in ProductInput) (*Product, error) {
	if err := in.normalise(); err != nil {
		return nil, err
	}

	var p Product
	err := s.pool.QueryRow(ctx, `
		INSERT INTO products (sku, barcode, name, description, price_kobo, compare_at_kobo,
		                      stock, image_url, category, is_new, active)
		VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, sku, COALESCE(barcode,''), name, description, price_kobo,
		          compare_at_kobo, stock, image_url, category, rating, review_count, is_new`,
		in.SKU, in.Barcode, in.Name, in.Description, in.PriceKobo, in.CompareAtKobo,
		in.Stock, in.ImageURL, in.Category, in.IsNew, in.Active).
		Scan(&p.ID, &p.SKU, &p.Barcode, &p.Name, &p.Description, &p.PriceKobo,
			&p.CompareAtKobo, &p.Stock, &p.ImageURL, &p.Category, &p.Rating,
			&p.ReviewCount, &p.IsNew)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrSKUTaken
	}
	if err != nil {
		return nil, fmt.Errorf("store: create product: %w", err)
	}
	return &p, nil
}

// UpdateProduct edits a product.
//
// Note what this does NOT touch: the price on existing order_items. Those are
// captured at purchase time on purpose, so repricing a product never rewrites
// what a past customer was charged.
func (s *Store) UpdateProduct(ctx context.Context, id int64, in ProductInput) (*Product, error) {
	if err := in.normalise(); err != nil {
		return nil, err
	}

	var p Product
	err := s.pool.QueryRow(ctx, `
		UPDATE products SET
			sku = $2, barcode = NULLIF($3,''), name = $4, description = $5,
			price_kobo = $6, compare_at_kobo = $7, stock = $8,
			image_url = $9, category = $10, is_new = $11, active = $12
		WHERE id = $1
		RETURNING id, sku, COALESCE(barcode,''), name, description, price_kobo,
		          compare_at_kobo, stock, image_url, category, rating, review_count, is_new`,
		id, in.SKU, in.Barcode, in.Name, in.Description, in.PriceKobo, in.CompareAtKobo,
		in.Stock, in.ImageURL, in.Category, in.IsNew, in.Active).
		Scan(&p.ID, &p.SKU, &p.Barcode, &p.Name, &p.Description, &p.PriceKobo,
			&p.CompareAtKobo, &p.Stock, &p.ImageURL, &p.Category, &p.Rating,
			&p.ReviewCount, &p.IsNew)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrSKUTaken
	}
	if err != nil {
		return nil, fmt.Errorf("store: update product: %w", err)
	}
	return &p, nil
}

// DeleteProduct removes a product from the shop.
//
// A product with orders against it is deactivated rather than deleted. Hard
// deletion would cascade the order_items away and quietly rewrite history:
// past receipts would lose their lines and the books would stop adding up.
// Only a product nobody ever bought is genuinely removed.
func (s *Store) DeleteProduct(ctx context.Context, id int64) (deactivated bool, err error) {
	var orderCount int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM order_items WHERE product_id = $1`, id).Scan(&orderCount); err != nil {
		return false, err
	}

	if orderCount > 0 {
		ct, err := s.pool.Exec(ctx, `UPDATE products SET active = FALSE WHERE id = $1`, id)
		if err != nil {
			return false, err
		}
		if ct.RowsAffected() == 0 {
			return false, ErrNotFound
		}
		return true, nil
	}

	ct, err := s.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	if ct.RowsAffected() == 0 {
		return false, ErrNotFound
	}
	return false, nil
}

// ListAllProducts includes inactive rows, for the admin view.
func (s *Store) ListAllProducts(ctx context.Context) ([]Product, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, sku, COALESCE(barcode,''), name, description, price_kobo,
		       compare_at_kobo, stock, image_url, category, rating, review_count, is_new, active
		FROM products ORDER BY active DESC, category, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Product{}
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.SKU, &p.Barcode, &p.Name, &p.Description,
			&p.PriceKobo, &p.CompareAtKobo, &p.Stock, &p.ImageURL, &p.Category,
			&p.Rating, &p.ReviewCount, &p.IsNew, &p.Active); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
