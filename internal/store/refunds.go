package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNotRefundable is returned when an order cannot be refunded.
var ErrNotRefundable = errors.New("store: order is not in a refundable state")

type Refund struct {
	ID             int64      `json:"id"`
	OrderID        int64      `json:"orderId"`
	OrderRef       string     `json:"orderReference,omitempty"`
	Reference      string     `json:"reference"`
	TransactionRef string     `json:"transactionRef"`
	AmountKobo     int64      `json:"amountKobo"`
	Reason         string     `json:"reason"`
	Status         string     `json:"status"`
	MonnifyComment string     `json:"monnifyComment,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
}

// BeginRefund records a refund attempt before Monnify is called.
//
// The row is written first, deliberately. If we called Monnify first and then
// crashed, money would have moved with no record of it. A pending row that
// turns out to have failed is recoverable; a silent refund is not.
//
// The UNIQUE constraint on reference makes a double-submitted refund button
// impossible to act on twice.
func (s *Store) BeginRefund(ctx context.Context, orderRef, refundRef, reason string, amountKobo int64, issuedBy int64) (*Refund, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var orderID int64
	var status, transactionRef string
	var totalKobo int64
	err = tx.QueryRow(ctx, `
		SELECT id, status::text, COALESCE(transaction_ref,''), total_kobo
		FROM orders WHERE reference = $1 FOR UPDATE`, orderRef).
		Scan(&orderID, &status, &transactionRef, &totalKobo)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// Only a paid order can be refunded. Refunding a pending one would return
	// money that never arrived.
	if status != "paid" {
		return nil, fmt.Errorf("%w: order is %s, not paid", ErrNotRefundable, status)
	}
	if transactionRef == "" {
		return nil, fmt.Errorf("%w: no Monnify transaction reference on this order", ErrNotRefundable)
	}

	// Never refund more than was collected, across all attempts.
	var alreadyRefunded int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_kobo), 0) FROM refunds
		WHERE order_id = $1 AND status <> 'failed'`, orderID).Scan(&alreadyRefunded); err != nil {
		return nil, err
	}
	if alreadyRefunded+amountKobo > totalKobo {
		return nil, fmt.Errorf("%w: refunding %d kobo would exceed the %d kobo paid (%d already refunded)",
			ErrNotRefundable, amountKobo, totalKobo, alreadyRefunded)
	}

	var rf Refund
	err = tx.QueryRow(ctx, `
		INSERT INTO refunds (order_id, reference, transaction_ref, amount_kobo, reason, issued_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, order_id, reference, transaction_ref, amount_kobo, reason, status::text, created_at`,
		orderID, refundRef, transactionRef, amountKobo, reason, nullableID(issuedBy)).
		Scan(&rf.ID, &rf.OrderID, &rf.Reference, &rf.TransactionRef, &rf.AmountKobo,
			&rf.Reason, &rf.Status, &rf.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: begin refund: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	rf.OrderRef = orderRef
	return &rf, nil
}

func nullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// SettleRefund records the outcome of a refund.
//
// A completed full refund moves the order to 'refunded' and returns the stock,
// since the goods were not sold after all. A partial refund leaves the order
// paid: some of it stands.
func (s *Store) SettleRefund(ctx context.Context, refundRef, status, comment string) (*Refund, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var rf Refund
	err = tx.QueryRow(ctx, `
		UPDATE refunds SET
			status = $2::refund_status,
			monnify_comment = $3,
			completed_at = CASE WHEN $2 = 'completed' THEN now() ELSE NULL END
		WHERE reference = $1
		RETURNING id, order_id, reference, transaction_ref, amount_kobo, reason,
		          status::text, monnify_comment, created_at, completed_at`,
		refundRef, status, comment).
		Scan(&rf.ID, &rf.OrderID, &rf.Reference, &rf.TransactionRef, &rf.AmountKobo,
			&rf.Reason, &rf.Status, &rf.MonnifyComment, &rf.CreatedAt, &rf.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if status == "completed" {
		var totalKobo int64
		if err := tx.QueryRow(ctx,
			`SELECT total_kobo FROM orders WHERE id = $1`, rf.OrderID).Scan(&totalKobo); err != nil {
			return nil, err
		}

		var refundedTotal int64
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(SUM(amount_kobo),0) FROM refunds
			WHERE order_id = $1 AND status = 'completed'`, rf.OrderID).Scan(&refundedTotal); err != nil {
			return nil, err
		}

		if refundedTotal >= totalKobo {
			if _, err := tx.Exec(ctx,
				`UPDATE orders SET status = 'refunded' WHERE id = $1`, rf.OrderID); err != nil {
				return nil, err
			}
			// The goods are coming back, so the stock does too.
			if _, err := tx.Exec(ctx, `
				UPDATE products p SET stock = p.stock + oi.quantity
				FROM order_items oi
				WHERE oi.order_id = $1 AND p.id = oi.product_id`, rf.OrderID); err != nil {
				return nil, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &rf, nil
}

// RefundsForOrder lists refund attempts against an order.
func (s *Store) RefundsForOrder(ctx context.Context, orderID int64) ([]Refund, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, order_id, reference, transaction_ref, amount_kobo, reason,
		       status::text, monnify_comment, created_at, completed_at
		FROM refunds WHERE order_id = $1 ORDER BY created_at DESC`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Refund{}
	for rows.Next() {
		var rf Refund
		if err := rows.Scan(&rf.ID, &rf.OrderID, &rf.Reference, &rf.TransactionRef,
			&rf.AmountKobo, &rf.Reason, &rf.Status, &rf.MonnifyComment,
			&rf.CreatedAt, &rf.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, rf)
	}
	return out, rows.Err()
}

// RecentRefunds lists refunds for the admin view.
func (s *Store) RecentRefunds(ctx context.Context, limit int) ([]Refund, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.order_id, o.reference, r.reference, r.transaction_ref,
		       r.amount_kobo, r.reason, r.status::text, r.monnify_comment,
		       r.created_at, r.completed_at
		FROM refunds r JOIN orders o ON o.id = r.order_id
		ORDER BY r.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Refund{}
	for rows.Next() {
		var rf Refund
		if err := rows.Scan(&rf.ID, &rf.OrderID, &rf.OrderRef, &rf.Reference, &rf.TransactionRef,
			&rf.AmountKobo, &rf.Reason, &rf.Status, &rf.MonnifyComment,
			&rf.CreatedAt, &rf.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, rf)
	}
	return out, rows.Err()
}
