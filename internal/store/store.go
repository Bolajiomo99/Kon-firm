// Package store is the persistence layer for Kon-firm.
//
// Money is int64 kobo throughout. It is converted to decimal naira only when
// talking to Monnify, and back on the way in.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrAlreadyProcessed signals that a webhook event was seen before and was
// therefore not applied again. Callers should treat this as success and
// return 2xx to Monnify: it means the ledger is doing its job, not that
// anything went wrong.
var ErrAlreadyProcessed = errors.New("store: webhook event already processed")

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("store: not found")

// ErrPriceChanged means a product's price moved between the shopper being
// quoted and the order being created. Refusing is the only honest option:
// charging the new price bills them for something they never agreed to, and
// charging the old one sells at a price the shop has withdrawn.
var ErrPriceChanged = errors.New("store: price changed during checkout")

type Store struct{ pool *pgxpool.Pool }

func New(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: bad DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Pool exposes the underlying pool for migrations and health checks.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

type Product struct {
	ID          int64  `json:"id"`
	SKU         string `json:"sku"`
	Barcode     string `json:"barcode,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PriceKobo   int64  `json:"priceKobo"`
	// CompareAtKobo is the pre-sale price, nil when nothing is discounted.
	// A pointer rather than 0 so "not on sale" and "free" cannot be confused.
	CompareAtKobo *int64   `json:"compareAtKobo,omitempty"`
	Stock         int      `json:"stock"`
	ImageURL      string   `json:"imageUrl"`
	Category      string   `json:"category"`
	Rating        *float64 `json:"rating,omitempty"`
	ReviewCount   int      `json:"reviewCount"`
	IsNew         bool     `json:"isNew"`
	// Active is only meaningful in the admin view; the storefront never sees
	// an inactive product.
	Active bool `json:"active"`
}

type OrderItem struct {
	ProductID     int64  `json:"productId"`
	ProductName   string `json:"productName"`
	Quantity      int    `json:"quantity"`
	UnitPriceKobo int64  `json:"unitPriceKobo"`
}

type Order struct {
	Quote          Quote       `json:"quote"`
	ID             int64       `json:"id"`
	Reference      string      `json:"reference"`
	TransactionRef string      `json:"transactionRef,omitempty"`
	CustomerName   string      `json:"customerName"`
	CustomerEmail  string      `json:"customerEmail"`
	TotalKobo      int64       `json:"totalKobo"`
	Status         string      `json:"status"`
	Channel        string      `json:"channel"`
	CheckoutURL    string      `json:"checkoutUrl,omitempty"`
	AmountPaidKobo *int64      `json:"amountPaidKobo,omitempty"`
	PaymentMethod  string      `json:"paymentMethod,omitempty"`
	CreatedAt      time.Time   `json:"createdAt"`
	PaidAt         *time.Time  `json:"paidAt,omitempty"`
	Items          []OrderItem `json:"items,omitempty"`

	DeliveryAddress string `json:"deliveryAddress,omitempty"`
	DeliveryCity    string `json:"deliveryCity,omitempty"`
	DeliveryState   string `json:"deliveryState,omitempty"`
	DeliveryPhone   string `json:"deliveryPhone,omitempty"`
}

func (s *Store) ListProducts(ctx context.Context) ([]Product, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, sku, COALESCE(barcode,''), name, description, price_kobo,
		       compare_at_kobo, stock, image_url, category, rating, review_count, is_new
		FROM products WHERE active = TRUE
		ORDER BY category, is_new DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.SKU, &p.Barcode, &p.Name, &p.Description,
			&p.PriceKobo, &p.CompareAtKobo, &p.Stock, &p.ImageURL,
			&p.Category, &p.Rating, &p.ReviewCount, &p.IsNew); err != nil {
			return nil, err
		}
		p.Active = true // this query filters on active already
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProductByBarcode backs the POS scanner.
func (s *Store) ProductByBarcode(ctx context.Context, barcode string) (*Product, error) {
	var p Product
	err := s.pool.QueryRow(ctx, `
		SELECT id, sku, COALESCE(barcode,''), name, description, price_kobo,
		       compare_at_kobo, stock, image_url, category, rating, review_count, is_new
		FROM products WHERE barcode = $1 AND active = TRUE`, barcode).
		Scan(&p.ID, &p.SKU, &p.Barcode, &p.Name, &p.Description, &p.PriceKobo,
			&p.CompareAtKobo, &p.Stock, &p.ImageURL, &p.Category, &p.Rating,
			&p.ReviewCount, &p.IsNew)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreateOrderLine is one requested line item, before pricing.
type CreateOrderLine struct {
	ProductID int64
	Quantity  int
}

// Delivery is where an online order is going.
type Delivery struct {
	Phone   string
	Address string
	City    string
	State   string
	Lat     *float64
	Lng     *float64
}

// CreateOrder prices a cart server-side and persists it as a pending order.
//
// Prices are read from the database inside the transaction; the client sends
// only product IDs and quantities. A browser that posts its own prices must
// never be believed, or a hostile cart pays ₦1 for a ₦100,000 item.
//
// quote carries the figures the caller already computed — discount, delivery,
// VAT — and they are frozen onto the order. The line prices are re-read here
// under FOR UPDATE regardless, and the resulting subtotal is checked against
// the quote: if they disagree, a price moved between quoting and paying, and
// the order is refused rather than charged at a figure nobody agreed to.
func (s *Store) CreateOrder(ctx context.Context, ref, name, email, channel string,
	lines []CreateOrderLine, quote Quote, del Delivery) (*Order, error) {

	if len(lines) == 0 {
		return nil, fmt.Errorf("store: cannot create an empty order")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var total int64
	items := make([]OrderItem, 0, len(lines))

	for _, l := range lines {
		if err := ValidateQuantity(l.Quantity); err != nil {
			return nil, err
		}

		var name string
		var price int64
		var stock int
		err := tx.QueryRow(ctx, `
			SELECT name, price_kobo, stock FROM products
			WHERE id = $1 AND active = TRUE FOR UPDATE`, l.ProductID).Scan(&name, &price, &stock)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("store: product %d not found: %w", l.ProductID, ErrNotFound)
		}
		if err != nil {
			return nil, err
		}
		if stock < l.Quantity {
			return nil, fmt.Errorf("store: insufficient stock for %q: have %d, want %d", name, stock, l.Quantity)
		}

		total += price * int64(l.Quantity)
		items = append(items, OrderItem{
			ProductID: l.ProductID, ProductName: name,
			Quantity: l.Quantity, UnitPriceKobo: price,
		})
	}

	// The subtotal just recomputed under FOR UPDATE must match what the
	// shopper was quoted. A mismatch means a price changed underneath them.
	if total != quote.SubtotalKobo {
		return nil, fmt.Errorf("%w: a price changed while you were checking out — please review your basket", ErrPriceChanged)
	}

	var o Order
	err = tx.QueryRow(ctx, `
		INSERT INTO orders (
			reference, customer_name, customer_email, total_kobo, channel,
			subtotal_kobo, discount_kobo, delivery_fee_kobo, vat_kobo, vat_rate_bp, voucher_code,
			delivery_name, delivery_phone, delivery_address, delivery_city, delivery_state,
			delivery_lat, delivery_lng
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING id, reference, customer_name, customer_email, total_kobo, status::text, channel::text, created_at`,
		ref, name, email, quote.TotalKobo, channel,
		quote.SubtotalKobo, quote.DiscountKobo, quote.DeliveryFeeKobo, quote.VATKobo, quote.VATRateBP, quote.VoucherCode,
		name, del.Phone, del.Address, del.City, del.State, del.Lat, del.Lng).
		Scan(&o.ID, &o.Reference, &o.CustomerName, &o.CustomerEmail, &o.TotalKobo, &o.Status, &o.Channel, &o.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: insert order: %w", err)
	}
	o.Quote = quote

	for _, it := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_items (order_id, product_id, quantity, unit_price_kobo, product_name)
			VALUES ($1,$2,$3,$4,$5)`,
			o.ID, it.ProductID, it.Quantity, it.UnitPriceKobo, it.ProductName); err != nil {
			return nil, fmt.Errorf("store: insert order item: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	o.Items = items
	return &o, nil
}

// AttachCheckout records what Monnify returned from init-transaction.
func (s *Store) AttachCheckout(ctx context.Context, ref, transactionRef, checkoutURL string) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE orders SET transaction_ref = $2, checkout_url = $3
		WHERE reference = $1 AND status = 'pending'`, ref, transactionRef, checkoutURL)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PaymentResult is the outcome reported by a verified webhook.
type PaymentResult struct {
	TransactionRef string
	PaymentRef     string
	EventType      string
	AmountPaidKobo int64
	PaymentMethod  string
	PaidAt         time.Time
	Success        bool
	RawPayload     []byte
	// OverrideTransactionRef replaces the order's stored reference with
	// TransactionRef instead of only filling it when empty.
	//
	// Online payments settle against the same reference the order was created
	// with, so the default COALESCE is right for them. An offline cash payment
	// is a DIFFERENT Monnify transaction from the abandoned online checkout, and
	// its reference is the one that actually got paid — so for offline the
	// stored reference must move to it, or the order looks unpaid to anyone who
	// verifies it against Monnify by the reference we kept.
	OverrideTransactionRef bool
}

// ApplyWebhook records an event and settles the order in one transaction.
//
// Idempotency is enforced by the UNIQUE (transaction_ref, event_type) index on
// webhook_events, not by a prior SELECT. A check-then-act would race: two
// concurrent redeliveries could both observe "unseen" and both credit the
// order. Here the loser of the INSERT race gets ErrAlreadyProcessed and the
// order is touched exactly once.
//
// ErrAlreadyProcessed is not a failure. Return 2xx to Monnify when you see it.
func (s *Store) ApplyWebhook(ctx context.Context, r PaymentResult) (*Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	ct, err := tx.Exec(ctx, `
		INSERT INTO webhook_events (transaction_ref, event_type, payload)
		VALUES ($1, $2, $3)
		ON CONFLICT (transaction_ref, event_type) DO NOTHING`,
		r.TransactionRef, r.EventType, r.RawPayload)
	if err != nil {
		return nil, fmt.Errorf("store: record webhook: %w", err)
	}
	if ct.RowsAffected() == 0 {
		// Seen before. Commit so the (empty) transaction closes cleanly, and
		// tell the caller not to give value twice.
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, ErrAlreadyProcessed
	}

	status := "failed"
	if r.Success {
		status = "paid"
	}

	var o Order
	// The casts on $2 and $5 are load-bearing: inside CASE ... ELSE NULL,
	// Postgres cannot infer a parameter's type and defaults it to text, which
	// then fails against a timestamptz column.
	err = tx.QueryRow(ctx, `
		UPDATE orders SET
			status = $2::order_status,
			amount_paid_kobo = $3,
			payment_method = $4,
			paid_at = CASE WHEN $2::order_status = 'paid' THEN $5::timestamptz ELSE NULL END,
			transaction_ref = CASE WHEN $7 THEN $6 ELSE COALESCE(transaction_ref, $6) END
		WHERE reference = $1 AND status = 'pending'
		RETURNING id, reference, COALESCE(transaction_ref,''), customer_name, customer_email,
		          total_kobo, status::text, channel::text, amount_paid_kobo,
		          payment_method, created_at, paid_at`,
		r.PaymentRef, status, r.AmountPaidKobo, r.PaymentMethod, r.PaidAt, r.TransactionRef, r.OverrideTransactionRef).
		Scan(&o.ID, &o.Reference, &o.TransactionRef, &o.CustomerName, &o.CustomerEmail,
			&o.TotalKobo, &o.Status, &o.Channel, &o.AmountPaidKobo,
			&o.PaymentMethod, &o.CreatedAt, &o.PaidAt)

	if errors.Is(err, pgx.ErrNoRows) {
		// The event is new but the order is not pending — already settled by a
		// different event type, or the reference is unknown. Keep the ledger
		// row (it is evidence) and report rather than silently ignoring.
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("store: no pending order for reference %q: %w", r.PaymentRef, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("store: settle order: %w", err)
	}

	// Decrement stock only on a real payment.
	if r.Success {
		if _, err := tx.Exec(ctx, `
			UPDATE products p SET stock = p.stock - oi.quantity
			FROM order_items oi
			WHERE oi.order_id = $1 AND p.id = oi.product_id`, o.ID); err != nil {
			return nil, fmt.Errorf("store: decrement stock: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &o, nil
}

// OrderByTransactionRef loads an order by MONNIFY's reference rather than ours.
//
// Almost everything in this codebase looks orders up by our own reference. The
// offline requery endpoint is the exception: Monnify asks "what happened to
// MNFY|44|20220407112123|000684?", which is their identifier, and the only
// place it exists on our side is the transaction_ref we recorded when the
// payment settled.
//
// An order that has never been paid has an empty transaction_ref, so the
// emptiness guard matters: without it, a requery carrying a blank reference
// would match an arbitrary unpaid order.
func (s *Store) OrderByTransactionRef(ctx context.Context, txRef string) (*Order, error) {
	if strings.TrimSpace(txRef) == "" {
		return nil, ErrNotFound
	}
	var ref string
	err := s.pool.QueryRow(ctx,
		`SELECT reference FROM orders WHERE transaction_ref = $1`, txRef).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.OrderByReference(ctx, ref)
}

// OrderByReference loads an order and its items.
func (s *Store) OrderByReference(ctx context.Context, ref string) (*Order, error) {
	var o Order
	err := s.pool.QueryRow(ctx, `
		SELECT id, reference, COALESCE(transaction_ref,''), customer_name, customer_email,
		       total_kobo, status::text, channel::text, checkout_url, amount_paid_kobo,
		       payment_method, created_at, paid_at,
		       subtotal_kobo, discount_kobo, delivery_fee_kobo, vat_kobo, vat_rate_bp, voucher_code,
		       delivery_address, delivery_city, delivery_state, delivery_phone
		FROM orders WHERE reference = $1`, ref).
		Scan(&o.ID, &o.Reference, &o.TransactionRef, &o.CustomerName, &o.CustomerEmail,
			&o.TotalKobo, &o.Status, &o.Channel, &o.CheckoutURL, &o.AmountPaidKobo,
			&o.PaymentMethod, &o.CreatedAt, &o.PaidAt,
			&o.Quote.SubtotalKobo, &o.Quote.DiscountKobo, &o.Quote.DeliveryFeeKobo,
			&o.Quote.VATKobo, &o.Quote.VATRateBP, &o.Quote.VoucherCode,
			&o.DeliveryAddress, &o.DeliveryCity, &o.DeliveryState, &o.DeliveryPhone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// Derived, not stored: the total is the order's own figure, and free
	// delivery is simply a zero fee.
	o.Quote.TotalKobo = o.TotalKobo
	o.Quote.FreeDelivery = o.Quote.DeliveryFeeKobo == 0

	rows, err := s.pool.Query(ctx, `
		SELECT product_id, product_name, quantity, unit_price_kobo
		FROM order_items WHERE order_id = $1 ORDER BY id`, o.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var it OrderItem
		if err := rows.Scan(&it.ProductID, &it.ProductName, &it.Quantity, &it.UnitPriceKobo); err != nil {
			return nil, err
		}
		o.Items = append(o.Items, it)
	}
	return &o, rows.Err()
}
