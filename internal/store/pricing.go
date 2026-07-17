package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Nigerian VAT.
//
// 7.5% since February 2020, and the Nigeria Tax Act 2025 — effective 1 January
// 2026 — kept it there despite proposals to raise it. Fashion and gadgets are
// standard-rated; the Act's zero-rating and exemptions cover food, medicines,
// education and passenger transport, none of which we sell.
//
// Held in basis points because 7.5 is not representable in binary floating
// point, and a tax figure that is off by a kobo is a tax figure that is wrong.
const VATRateBP int64 = 750

// Delivery.
//
// Free over ₦50,000 in Lagos, which is what the storefront promises. If these
// ever disagree, the storefront is lying to the customer — so the promise and
// the arithmetic live next to each other deliberately.
const (
	LagosDeliveryFeeKobo      int64 = 200000  // ₦2,000
	NationwideDeliveryKobo    int64 = 450000  // ₦4,500
	FreeDeliveryThresholdKobo int64 = 5000000 // ₦50,000
)

// Sanity bounds on a basket.
//
// Not arbitrary politeness: without an upper bound, price × quantity overflows
// int64 and wraps NEGATIVE — 3,400,000 × 9,223,372,036,854,775,807 is
// -3,400,000. Go does not panic on integer overflow, so the arithmetic
// silently produces a wrong, plausible-looking number instead of an error.
//
// The stock check would refuse such an order anyway, so no money was ever at
// risk. But a quote endpoint that will happily report ₦33 trillion is a bug on
// its own, and relying on a check two layers away to save the arithmetic is
// how the layer without the check eventually gets called from somewhere new.
const (
	// 10,000 of one item is far beyond any real order and far below overflow.
	MaxQuantity = 10_000
	// 50 distinct products in one basket.
	MaxBasketLines = 50
	// ₦100,000,000. Above this it is not a shopping basket.
	MaxBasketKobo int64 = 10_000_000_000
)

// ErrUnreasonableBasket means the basket is outside sane bounds.
var ErrUnreasonableBasket = errors.New("store: unreasonable basket")

// ValidateQuantity bounds a line quantity.
func ValidateQuantity(q int) error {
	if q <= 0 {
		return fmt.Errorf("%w: quantity must be at least 1", ErrUnreasonableBasket)
	}
	if q > MaxQuantity {
		return fmt.Errorf("%w: at most %d of one item per order", ErrUnreasonableBasket, MaxQuantity)
	}
	return nil
}

var (
	ErrVoucherNotFound = errors.New("store: that voucher code is not valid")
	ErrVoucherExpired  = errors.New("store: that voucher has expired")
	ErrVoucherUsedUp   = errors.New("store: that voucher has been fully redeemed")
	ErrVoucherMinSpend = errors.New("store: order is below this voucher's minimum spend")
)

// VATWithin returns the VAT contained in a VAT-inclusive amount.
//
// Shelf prices include VAT, as Nigerian retail expects, so the tax is
// extracted rather than added: vat = gross × rate / (100% + rate). Adding
// 7.5% at checkout would change the number the customer already agreed to,
// which is precisely the surprise this avoids.
//
// Integer arithmetic throughout, rounded half-up at the final division.
func VATWithin(grossKobo, rateBP int64) int64 {
	if grossKobo <= 0 || rateBP <= 0 {
		return 0
	}
	num := grossKobo * rateBP
	den := 10000 + rateBP
	return (num + den/2) / den
}

// DeliveryFee returns the fee for an order.
func DeliveryFee(subtotalKobo int64, state string) int64 {
	if subtotalKobo >= FreeDeliveryThresholdKobo && isLagos(state) {
		return 0
	}
	if isLagos(state) {
		return LagosDeliveryFeeKobo
	}
	return NationwideDeliveryKobo
}

func isLagos(state string) bool {
	return strings.EqualFold(strings.TrimSpace(state), "Lagos")
}

// Voucher is a discount code.
type Voucher struct {
	Code            string
	Kind            string // percent | fixed
	Value           int64  // basis points, or kobo
	MaxDiscountKobo *int64
	MinSpendKobo    int64
	MaxUses         *int
	TimesUsed       int
	ExpiresAt       *time.Time
}

// DiscountFor computes what this voucher takes off a subtotal.
//
// The discount can never exceed the subtotal: a voucher worth more than the
// basket must not produce a negative total, which would mean paying the
// customer to shop.
func (v *Voucher) DiscountFor(subtotalKobo int64) int64 {
	var d int64
	switch v.Kind {
	case "percent":
		if subtotalKobo <= 0 || v.Value <= 0 {
			d = 0
		break
		}
		// Percent vouchers are computed in basis points. Guard the multiply so a
		// huge value cannot overflow and wrap to a negative discount before we
		// clamp it to the basket.
		raw := subtotalKobo * v.Value
		if raw < 0 || raw/v.Value != subtotalKobo {
			d = subtotalKobo
			break
		}
		d = (raw + 5000) / 10000
	case "fixed":
		d = v.Value
	}
	if v.MaxDiscountKobo != nil && d > *v.MaxDiscountKobo {
		d = *v.MaxDiscountKobo
	}
	if d > subtotalKobo {
		d = subtotalKobo
	}
	if d < 0 {
		d = 0
	}
	return d
}

// VoucherByCode loads and validates a voucher against a subtotal.
func (s *Store) VoucherByCode(ctx context.Context, code string, subtotalKobo int64) (*Voucher, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, ErrVoucherNotFound
	}

	var v Voucher
	err := s.pool.QueryRow(ctx, `
		SELECT code, kind::text, value, max_discount_kobo, min_spend_kobo,
		       max_uses, times_used, expires_at
		FROM vouchers WHERE code = $1 AND active = TRUE`, code).
		Scan(&v.Code, &v.Kind, &v.Value, &v.MaxDiscountKobo, &v.MinSpendKobo,
			&v.MaxUses, &v.TimesUsed, &v.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVoucherNotFound
	}
	if err != nil {
		return nil, err
	}

	if v.ExpiresAt != nil && time.Now().After(*v.ExpiresAt) {
		return nil, ErrVoucherExpired
	}
	if v.MaxUses != nil && v.TimesUsed >= *v.MaxUses {
		return nil, ErrVoucherUsedUp
	}
	if subtotalKobo < v.MinSpendKobo {
		return nil, fmt.Errorf("%w: spend at least %d kobo", ErrVoucherMinSpend, v.MinSpendKobo)
	}
	return &v, nil
}

// Quote is a full price breakdown. Every field is server-computed.
type Quote struct {
	SubtotalKobo    int64  `json:"subtotalKobo"`
	DiscountKobo    int64  `json:"discountKobo"`
	DeliveryFeeKobo int64  `json:"deliveryFeeKobo"`
	TotalKobo       int64  `json:"totalKobo"`
	VATKobo         int64  `json:"vatKobo"`
	VATRateBP       int64  `json:"vatRateBp"`
	VoucherCode     string `json:"voucherCode,omitempty"`
	FreeDelivery    bool   `json:"freeDelivery"`
}

// BuildQuote prices a basket.
//
// Order of operations matters and is deliberate:
//  1. subtotal from database prices (VAT-inclusive)
//  2. discount off the subtotal
//  3. delivery assessed on the DISCOUNTED subtotal — a voucher that drops an
//     order below the free-delivery threshold must actually lose free delivery,
//     or the threshold means nothing
//  4. VAT extracted from the final total, since that is the sum actually charged
func BuildQuote(subtotalKobo, discountKobo, deliveryFeeKobo int64, voucherCode string) Quote {
	total := subtotalKobo - discountKobo + deliveryFeeKobo
	if total < 0 {
		total = 0
	}
	return Quote{
		SubtotalKobo:    subtotalKobo,
		DiscountKobo:    discountKobo,
		DeliveryFeeKobo: deliveryFeeKobo,
		TotalKobo:       total,
		VATKobo:         VATWithin(total, VATRateBP),
		VATRateBP:       VATRateBP,
		VoucherCode:     voucherCode,
		FreeDelivery:    deliveryFeeKobo == 0,
	}
}

// RedeemVoucher increments the use count.
//
// Conditional on the count still being under the cap, so two simultaneous
// checkouts cannot both take the last use of a limited code.
func (s *Store) RedeemVoucher(ctx context.Context, code string) error {
	if code == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE vouchers SET times_used = times_used + 1
		WHERE code = $1 AND (max_uses IS NULL OR times_used < max_uses)`, code)
	return err
}

// PriceBasket returns the VAT-inclusive subtotal for a set of lines, priced
// from the database.
//
// Separate from CreateOrder so a quote and a checkout compute the subtotal the
// same way. If they used different code, the total shown to a shopper could
// drift from the total they are charged — which is the one bug in a shop that
// nobody forgives.
func (s *Store) PriceBasket(ctx context.Context, lines []CreateOrderLine) (int64, error) {
	if len(lines) == 0 {
		return 0, fmt.Errorf("store: empty basket")
	}
	if len(lines) > MaxBasketLines {
		return 0, fmt.Errorf("%w: a basket may hold at most %d different products", ErrUnreasonableBasket, MaxBasketLines)
	}

	var subtotal int64
	for _, l := range lines {
		if err := ValidateQuantity(l.Quantity); err != nil {
			return 0, err
		}
		var price int64
		err := s.pool.QueryRow(ctx,
			`SELECT price_kobo FROM products WHERE id = $1 AND active = TRUE`, l.ProductID).Scan(&price)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("store: product %d: %w", l.ProductID, ErrNotFound)
		}
		if err != nil {
			return 0, err
		}

		// Bounded quantities make this multiplication safe, but the check is
		// here anyway: an overflow does not error, it silently wraps to a
		// negative price, and a wrong number is worse than a rejected one.
		line := price * int64(l.Quantity)
		if line < 0 || (price > 0 && line/price != int64(l.Quantity)) {
			return 0, fmt.Errorf("%w: that quantity overflows", ErrUnreasonableBasket)
		}
		if subtotal > MaxBasketKobo-line {
			return 0, fmt.Errorf("%w: a basket may not exceed ₦%d", ErrUnreasonableBasket, MaxBasketKobo/100)
		}
		subtotal += line
	}
	return subtotal, nil
}
