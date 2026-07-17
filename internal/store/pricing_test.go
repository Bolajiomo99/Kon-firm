package store

import "testing"

// TestVATWithin checks VAT extraction from VAT-inclusive prices.
//
// Nigerian shelf prices include VAT, so the tax is extracted, not added.
// Getting this backwards inflates every total by 7.5% — and would show the
// customer a number they never agreed to at the exact moment they are asked
// to pay it.
func TestVATWithin(t *testing.T) {
	cases := []struct {
		name  string
		gross int64
		want  int64
	}{
		// ₦47,500.00 inclusive -> VAT = 47500 * 7.5/107.5 = ₦3,313.95
		{"sneaker", 4750000, 331395},
		// ₦107.50 inclusive -> exactly ₦7.50 VAT. The textbook case.
		{"clean round number", 10750, 750},
		{"zero", 0, 0},
		{"negative is not a refund path", -5000, 0},
		// ₦1.00 -> 7 kobo (6.976 rounds up).
		{"rounds half up", 100, 7},
		{"one kobo", 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := VATWithin(c.gross, VATRateBP); got != c.want {
				t.Errorf("VATWithin(%d) = %d, want %d", c.gross, got, c.want)
			}
		})
	}
}

// TestVATWithin_IsInclusiveNotAdditive is the guard against the whole idea
// being inverted by a later "fix".
func TestVATWithin_IsInclusiveNotAdditive(t *testing.T) {
	const gross = 10750 // ₦107.50
	vat := VATWithin(gross, VATRateBP)

	// The net plus its VAT must reconstruct the gross exactly.
	net := gross - vat
	if net+vat != gross {
		t.Fatalf("net(%d) + vat(%d) = %d, want %d", net, vat, net+vat, gross)
	}
	// And the VAT must be 7.5% OF THE NET, not of the gross.
	if net != 10000 {
		t.Errorf("net = %d, want 10000 — VAT is being added rather than extracted", net)
	}
	if added := gross * VATRateBP / 10000; vat == added {
		t.Error("VAT equals 7.5% of the gross; it should be 7.5% of the net")
	}
}

func TestDeliveryFee(t *testing.T) {
	cases := []struct {
		name     string
		subtotal int64
		state    string
		want     int64
	}{
		{"lagos over threshold is free", 5000000, "Lagos", 0},
		{"lagos well over is free", 9900000, "Lagos", 0},
		{"lagos under threshold pays", 4999999, "Lagos", LagosDeliveryFeeKobo},
		{"lagos case-insensitive", 5000000, "lagos", 0},
		{"lagos with whitespace", 5000000, "  Lagos ", 0},
		// The free-delivery promise is Lagos-only; elsewhere always pays.
		{"abuja over threshold still pays", 9900000, "Abuja", NationwideDeliveryKobo},
		{"kano pays nationwide", 100000, "Kano", NationwideDeliveryKobo},
		{"empty state pays nationwide", 9900000, "", NationwideDeliveryKobo},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DeliveryFee(c.subtotal, c.state); got != c.want {
				t.Errorf("DeliveryFee(%d, %q) = %d, want %d", c.subtotal, c.state, got, c.want)
			}
		})
	}
}

func TestVoucher_DiscountFor(t *testing.T) {
	cap150k := int64(1500000)

	t.Run("percent", func(t *testing.T) {
		v := Voucher{Kind: "percent", Value: 1000} // 10%
		if got := v.DiscountFor(5000000); got != 500000 {
			t.Errorf("10%% of ₦50,000 = %d, want 500000", got)
		}
	})

	t.Run("percent respects its cap", func(t *testing.T) {
		v := Voucher{Kind: "percent", Value: 1000, MaxDiscountKobo: &cap150k}
		// 10% of ₦400,000 would be ₦40,000, but the cap is ₦15,000.
		if got := v.DiscountFor(40000000); got != cap150k {
			t.Errorf("capped discount = %d, want %d", got, cap150k)
		}
	})

	t.Run("fixed", func(t *testing.T) {
		v := Voucher{Kind: "fixed", Value: 500000}
		if got := v.DiscountFor(5000000); got != 500000 {
			t.Errorf("fixed discount = %d, want 500000", got)
		}
	})

	t.Run("never exceeds the basket", func(t *testing.T) {
		// A ₦5,000 voucher against a ₦1,000 basket must not pay the customer.
		v := Voucher{Kind: "fixed", Value: 500000}
		if got := v.DiscountFor(100000); got != 100000 {
			t.Errorf("discount = %d, want it clamped to the ₦1,000 subtotal", got)
		}
	})

	t.Run("unknown kind discounts nothing", func(t *testing.T) {
		v := Voucher{Kind: "mystery", Value: 999999}
		if got := v.DiscountFor(5000000); got != 0 {
			t.Errorf("unknown kind gave %d off; want 0", got)
		}
	})
}

func TestBuildQuote(t *testing.T) {
	t.Run("free delivery in Lagos over threshold", func(t *testing.T) {
		sub := int64(6000000) // ₦60,000
		q := BuildQuote(sub, 0, DeliveryFee(sub, "Lagos"), "")
		if !q.FreeDelivery || q.DeliveryFeeKobo != 0 {
			t.Errorf("expected free delivery, got fee %d", q.DeliveryFeeKobo)
		}
		if q.TotalKobo != sub {
			t.Errorf("total = %d, want %d", q.TotalKobo, sub)
		}
		if q.VATKobo != VATWithin(sub, VATRateBP) {
			t.Errorf("VAT = %d, want it extracted from the total", q.VATKobo)
		}
	})

	// The important one: a voucher that drops the basket under the threshold
	// must actually cost the shopper their free delivery, or the threshold is
	// decorative.
	t.Run("discount below threshold loses free delivery", func(t *testing.T) {
		sub := int64(5200000)      // ₦52,000 — qualifies
		discount := int64(1000000) // ₦10,000 off -> ₦42,000, no longer qualifies
		fee := DeliveryFee(sub-discount, "Lagos")
		q := BuildQuote(sub, discount, fee, "WELCOME10")

		if q.DeliveryFeeKobo != LagosDeliveryFeeKobo {
			t.Errorf("delivery fee = %d, want %d — the discount took it under the threshold",
				q.DeliveryFeeKobo, LagosDeliveryFeeKobo)
		}
		want := sub - discount + LagosDeliveryFeeKobo
		if q.TotalKobo != want {
			t.Errorf("total = %d, want %d", q.TotalKobo, want)
		}
	})

	t.Run("total is never negative", func(t *testing.T) {
		q := BuildQuote(100000, 500000, 0, "TOOBIG")
		if q.TotalKobo < 0 {
			t.Errorf("total = %d; a basket must never pay the customer", q.TotalKobo)
		}
	})

	t.Run("VAT is of the total actually charged", func(t *testing.T) {
		// Delivery is a taxable supply too, so VAT covers the whole charge.
		q := BuildQuote(4000000, 0, LagosDeliveryFeeKobo, "")
		total := int64(4000000 + LagosDeliveryFeeKobo)
		if q.TotalKobo != total {
			t.Fatalf("total = %d, want %d", q.TotalKobo, total)
		}
		if q.VATKobo != VATWithin(total, VATRateBP) {
			t.Errorf("VAT = %d, want %d — VAT must cover delivery as well",
				q.VATKobo, VATWithin(total, VATRateBP))
		}
	})
}
