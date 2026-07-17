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
		d = (subtotalKobo*v.Value + 5000) / 10000
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
