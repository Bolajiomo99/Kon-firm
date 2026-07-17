package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Bolajiomo99/Kon-firm/internal/config"
)

// testStore connects to the database named by DATABASE_URL and applies
// migrations. Tests are skipped when it is unset, so `go test ./...` still
// works on a machine with no database configured.
func testStore(t *testing.T) *Store {
	t.Helper()

	_ = config.LoadDotEnv(config.FindDotEnv())
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping database integration tests")
	}

	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)

	if err := Migrate(ctx, s.Pool()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

// seedProduct inserts a product unique to this test run and removes it after.
func seedProduct(t *testing.T, s *Store, stock int) int64 {
	t.Helper()
	ctx := context.Background()

	sku := fmt.Sprintf("TEST-SKU-%d", time.Now().UnixNano())
	var id int64
	err := s.Pool().QueryRow(ctx, `
		INSERT INTO products (sku, barcode, name, price_kobo, stock)
		VALUES ($1, $2, 'Test Widget', 250000, $3) RETURNING id`,
		sku, sku, stock).Scan(&id)
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}

	t.Cleanup(func() {
		_, _ = s.Pool().Exec(ctx, `DELETE FROM products WHERE id = $1`, id)
	})
	return id
}

// createTestOrder mirrors what the checkout handler does: price the basket,
// build a quote, then create. Tests that skip the quote would not exercise the
// price-change guard, which is the whole point of passing one in.
func createTestOrder(t *testing.T, s *Store, ref, name, email, channel string, lines []CreateOrderLine) (*Order, error) {
	t.Helper()
	ctx := context.Background()
	subtotal, err := s.PriceBasket(ctx, lines)
	if err != nil {
		return nil, err
	}
	q := BuildQuote(subtotal, 0, 0, "")
	return s.CreateOrder(ctx, ref, name, email, channel, lines, q, Delivery{
		Address: "12 Balogun Street", City: "Lagos Island", State: "Lagos",
	})
}

func cleanupOrder(t *testing.T, s *Store, ref string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = s.Pool().Exec(ctx, `DELETE FROM webhook_events WHERE transaction_ref LIKE $1`, ref+"%")
		_, _ = s.Pool().Exec(ctx, `DELETE FROM orders WHERE reference = $1`, ref)
	})
}

func TestCreateOrder_PricesServerSide(t *testing.T) {
	s := testStore(t)
	pid := seedProduct(t, s, 10)

	ref := fmt.Sprintf("KF-TEST-%d", time.Now().UnixNano())
	cleanupOrder(t, s, ref)

	o, err := createTestOrder(t, s, ref, "Ada Lovelace", "ada@example.com", "online",
		[]CreateOrderLine{{ProductID: pid, Quantity: 3}})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	// 3 x 250000 kobo = 750000 kobo (₦7,500). The client never sent a price.
	if o.TotalKobo != 750000 {
		t.Errorf("TotalKobo = %d, want 750000", o.TotalKobo)
	}
	if o.Status != "pending" {
		t.Errorf("Status = %q, want pending", o.Status)
	}
}

func TestCreateOrder_RejectsOverStock(t *testing.T) {
	s := testStore(t)
	pid := seedProduct(t, s, 2)

	ref := fmt.Sprintf("KF-TEST-%d", time.Now().UnixNano())
	cleanupOrder(t, s, ref)

	_, err := createTestOrder(t, s, ref, "Ada", "ada@example.com", "online",
		[]CreateOrderLine{{ProductID: pid, Quantity: 5}})
	if err == nil {
		t.Fatal("expected ordering 5 of a 2-stock item to fail")
	}
}

func TestCreateOrder_RejectsEmptyCart(t *testing.T) {
	s := testStore(t)
	_, err := s.CreateOrder(context.Background(), "KF-EMPTY", "A", "a@b.c", "online", nil, Quote{}, Delivery{})
	if err == nil {
		t.Fatal("expected empty cart to be rejected")
	}
}

// TestApplyWebhook_IsIdempotent is the load-bearing test of this project.
//
// Monnify redelivers notifications. If a redelivery credits an order twice,
// stock is decremented twice and the merchant's books are wrong. This proves
// the second delivery changes nothing.
func TestApplyWebhook_IsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	const startStock = 10
	pid := seedProduct(t, s, startStock)

	ref := fmt.Sprintf("KF-TEST-%d", time.Now().UnixNano())
	txRef := "MNFY-TEST-" + ref
	cleanupOrder(t, s, txRef)
	cleanupOrder(t, s, ref)

	if _, err := createTestOrder(t, s, ref, "Ada", "ada@example.com", "online",
		[]CreateOrderLine{{ProductID: pid, Quantity: 2}}); err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	result := PaymentResult{
		TransactionRef: txRef,
		PaymentRef:     ref,
		EventType:      "SUCCESSFUL_TRANSACTION",
		AmountPaidKobo: 500000,
		PaymentMethod:  "CARD",
		PaidAt:         time.Now().UTC(),
		Success:        true,
		RawPayload:     []byte(`{"eventType":"SUCCESSFUL_TRANSACTION"}`),
	}

	// First delivery: the order settles.
	o, err := s.ApplyWebhook(ctx, result)
	if err != nil {
		t.Fatalf("first ApplyWebhook: %v", err)
	}
	if o.Status != "paid" {
		t.Fatalf("after first webhook Status = %q, want paid", o.Status)
	}
	if o.PaidAt == nil {
		t.Error("PaidAt should be set on a paid order")
	}

	stockAfterFirst := currentStock(t, s, pid)
	if stockAfterFirst != startStock-2 {
		t.Fatalf("stock after first webhook = %d, want %d", stockAfterFirst, startStock-2)
	}

	// Second, identical delivery: must be recognised and must change nothing.
	_, err = s.ApplyWebhook(ctx, result)
	if !errors.Is(err, ErrAlreadyProcessed) {
		t.Fatalf("second ApplyWebhook error = %v, want ErrAlreadyProcessed", err)
	}

	stockAfterSecond := currentStock(t, s, pid)
	if stockAfterSecond != stockAfterFirst {
		t.Errorf("REPLAY DOUBLE-SPENT: stock went %d -> %d on a duplicate webhook",
			stockAfterFirst, stockAfterSecond)
	}

	// And exactly one ledger row exists for this event.
	var events int
	if err := s.Pool().QueryRow(ctx,
		`SELECT count(*) FROM webhook_events WHERE transaction_ref = $1`, txRef).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Errorf("webhook_events rows = %d, want exactly 1", events)
	}
}

// TestApplyWebhook_ConcurrentReplays hammers the same event from many
// goroutines. A check-then-act implementation passes the sequential test above
// and fails this one.
func TestApplyWebhook_ConcurrentReplays(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	const startStock = 50
	pid := seedProduct(t, s, startStock)

	ref := fmt.Sprintf("KF-TEST-CONC-%d", time.Now().UnixNano())
	txRef := "MNFY-TEST-" + ref
	cleanupOrder(t, s, txRef)
	cleanupOrder(t, s, ref)

	if _, err := createTestOrder(t, s, ref, "Ada", "ada@example.com", "online",
		[]CreateOrderLine{{ProductID: pid, Quantity: 3}}); err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	result := PaymentResult{
		TransactionRef: txRef,
		PaymentRef:     ref,
		EventType:      "SUCCESSFUL_TRANSACTION",
		AmountPaidKobo: 750000,
		PaymentMethod:  "CARD",
		PaidAt:         time.Now().UTC(),
		Success:        true,
		RawPayload:     []byte(`{"eventType":"SUCCESSFUL_TRANSACTION"}`),
	}

	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := s.ApplyWebhook(ctx, result)
			errs <- err
		}()
	}

	var applied, duplicates, other int
	for i := 0; i < n; i++ {
		switch err := <-errs; {
		case err == nil:
			applied++
		case errors.Is(err, ErrAlreadyProcessed):
			duplicates++
		default:
			other++
			t.Logf("unexpected error: %v", err)
		}
	}

	if applied != 1 {
		t.Errorf("applied = %d, want exactly 1 (the rest must be rejected as duplicates)", applied)
	}
	if duplicates != n-1-other {
		t.Errorf("duplicates = %d, applied = %d, other = %d (of %d)", duplicates, applied, other, n)
	}

	if got := currentStock(t, s, pid); got != startStock-3 {
		t.Errorf("stock = %d, want %d — concurrent replays decremented more than once", got, startStock-3)
	}
}

func currentStock(t *testing.T, s *Store, productID int64) int {
	t.Helper()
	var stock int
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT stock FROM products WHERE id = $1`, productID).Scan(&stock); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	return stock
}

// TestCreateOrder_RefusesWhenPriceMoved covers the window between a shopper
// being quoted and the order being written. Charging the new price bills them
// for something they never agreed to; charging the old one sells at a price
// the shop has withdrawn. Refusing is the only honest option.
func TestCreateOrder_RefusesWhenPriceMoved(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	pid := seedProduct(t, s, 10)

	ref := fmt.Sprintf("KF-TEST-PRICE-%d", time.Now().UnixNano())
	cleanupOrder(t, s, ref)

	lines := []CreateOrderLine{{ProductID: pid, Quantity: 1}}

	// Quote at the current price.
	subtotal, err := s.PriceBasket(ctx, lines)
	if err != nil {
		t.Fatal(err)
	}
	staleQuote := BuildQuote(subtotal, 0, 0, "")

	// The shop repricing the item mid-checkout.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE products SET price_kobo = price_kobo + 100000 WHERE id = $1`, pid); err != nil {
		t.Fatal(err)
	}

	_, err = s.CreateOrder(ctx, ref, "Ada", "ada@example.com", "online", lines, staleQuote, Delivery{
		Address: "12 Balogun Street", State: "Lagos",
	})
	if !errors.Is(err, ErrPriceChanged) {
		t.Fatalf("CreateOrder error = %v, want ErrPriceChanged — a stale quote must not be honoured", err)
	}
}
