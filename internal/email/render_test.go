package email

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestReceiptRenders(t *testing.T) {
	r := Receipt{
		CustomerName: "Zainab Wahab", Email: "z@example.com",
		Reference: "KF-1784276894-b64e34d9", MonnifyRef: "MNFY|04|20260717092815|000198",
		PaidAt: time.Now(), PaymentMethod: "Account transfer",
		Lines: []ReceiptLine{
			{Name: "Oxford Cotton Shirt", Quantity: 2, Amount: "₦49,000.00"},
			{Name: "Woven Leather Belt", Quantity: 1, Amount: "₦16,500.00"},
		},
		Subtotal: "₦65,500.00", Discount: "₦6,550.00", VoucherCode: "WELCOME10",
		Delivery: "₦2,000.00", FreeDelivery: false,
		Total: "₦60,950.00", VAT: "₦4,252.33", VATRate: "7.5%",
		Address: "12 Balogun Street, Lagos Island, Lagos",
		ReceiptURL: "https://konfirm.onrender.com/payment/callback?paymentReference=KF-1784276894-b64e34d9",
		Year: 2026,
	}
	var b bytes.Buffer
	if err := receiptTmpl.Execute(&b, r); err != nil {
		t.Fatalf("template: %v", err)
	}
	out := b.String()
	for _, want := range []string{"Zainab Wahab", "₦60,950.00", "WELCOME10", "₦4,252.33 at 7.5%",
		"Oxford Cotton Shirt", "12 Balogun Street", "MNFY|04|20260717092815|000198", "PAYMENT CONFIRMED"} {
		if !strings.Contains(out, want) {
			t.Errorf("receipt is missing %q", want)
		}
	}
	// A product name is customer-visible data; it must not be able to inject markup.
	r.Lines = []ReceiptLine{{Name: `<script>alert(1)</script>`, Quantity: 1, Amount: "₦1.00"}}
	b.Reset()
	_ = receiptTmpl.Execute(&b, r)
	if strings.Contains(b.String(), "<script>alert(1)</script>") {
		t.Error("a product name was injected into the receipt unescaped")
	}
	t.Logf("receipt renders: %d bytes", len(out))
}
