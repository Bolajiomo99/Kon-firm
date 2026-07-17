package monnify

import (
	"encoding/json"
	"testing"
	"time"
)

// realSandboxResponse is captured verbatim from the Monnify sandbox for
// transaction MNFY|41|20260717031412|000179 — a genuine ₦45,000 transfer.
//
// It is here because every bug in this package came from a fixture someone
// wrote by hand. Documented examples and real responses disagree about field
// names and about whether money is a number or a string. This is the real
// thing, so a future change that breaks against production breaks here first.
const realSandboxResponse = `{
  "transactionReference": "MNFY|41|20260717031412|000179",
  "paymentReference": "KF-1784254451-970d7097d6096f29",
  "amountPaid": "45000.00",
  "totalPayable": "45000.00",
  "settlementAmount": "44990.00",
  "paidOn": "2026-07-17 03:14:51.0",
  "paymentStatus": "PAID",
  "paymentDescription": "Kon-firm order KF-1784254451-970d7097d6096f29",
  "currency": "NGN",
  "paymentMethod": "ACCOUNT_TRANSFER",
  "cardDetails": null,
  "paymentScope": null,
  "metaData": {},
  "product": {"type": "WEB_SDK", "reference": "KF-1784254451-970d7097d6096f29"},
  "customer": {"email": "customer@example.com", "name": "Bolaji Jimoh"},
  "accountDetails": {
    "accountName": "Monnify Limited",
    "accountNumber": "******4015",
    "bankCode": "000015",
    "amountPaid": "45000.00"
  },
  "accountPayments": [
    {"accountName": "Monnify Limited", "accountNumber": "******4015", "amountPaid": "45000.00"}
  ]
}`

func TestTransaction_DecodesRealSandboxResponse(t *testing.T) {
	var tx Transaction
	if err := json.Unmarshal([]byte(realSandboxResponse), &tx); err != nil {
		t.Fatalf("decoding a real Monnify response must not fail: %v", err)
	}

	if tx.TransactionReference != "MNFY|41|20260717031412|000179" {
		t.Errorf("TransactionReference = %q", tx.TransactionReference)
	}
	if tx.PaymentReference != "KF-1784254451-970d7097d6096f29" {
		t.Errorf("PaymentReference = %q", tx.PaymentReference)
	}
	if !tx.Paid() {
		t.Errorf("Paid() = false for paymentStatus %q", tx.PaymentStatus)
	}

	// The string-vs-number bug: these arrive quoted.
	if got := tx.AmountPaid.Kobo(); got != 4500000 {
		t.Errorf("AmountPaid.Kobo() = %d, want 4500000 — quoted amounts must decode", got)
	}
	if got := tx.TotalPayable.Kobo(); got != 4500000 {
		t.Errorf("TotalPayable.Kobo() = %d, want 4500000", got)
	}
	if got := tx.SettlementAmount.Kobo(); got != 4499000 {
		t.Errorf("SettlementAmount.Kobo() = %d, want 4499000", got)
	}

	// ₦10 fee: 45000.00 paid, 44990.00 settled.
	if got := tx.FeeKobo(); got != 1000 {
		t.Errorf("FeeKobo() = %d, want 1000", got)
	}

	// The zero-timestamp bug: the field is paidOn, not completedOn.
	if tx.PaidOn.IsZero() {
		t.Fatal("paidOn did not decode — this rendered a receipt dated 0001-01-01")
	}
	want := time.Date(2026, 7, 17, 3, 14, 51, 0, time.UTC)
	if !tx.PaidOn.Equal(want) {
		t.Errorf("PaidOn = %v, want %v", tx.PaidOn.Time, want)
	}

	if tx.Customer.Name != "Bolaji Jimoh" {
		t.Errorf("Customer.Name = %q — customer is nested, not a top-level field", tx.Customer.Name)
	}
	if tx.PaymentMethod != "ACCOUNT_TRANSFER" {
		t.Errorf("PaymentMethod = %q", tx.PaymentMethod)
	}
}

func TestTransaction_PaidStatuses(t *testing.T) {
	cases := []struct {
		status string
		paid   bool
	}{
		{StatusPaid, true},
		{StatusOverpaid, true}, // paid at least the bill; withholding goods would be perverse
		{StatusPartiallyPaid, false},
		{StatusPending, false},
		{StatusFailed, false},
		{StatusExpired, false},
		{StatusCancelled, false},
		{"", false},
	}
	for _, c := range cases {
		tx := Transaction{PaymentStatus: c.status}
		if got := tx.Paid(); got != c.paid {
			t.Errorf("Paid() for %q = %v, want %v", c.status, got, c.paid)
		}
	}
}

func TestTransaction_PaidOnOrFallsBack(t *testing.T) {
	fallback := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	var unreadable Transaction
	if err := json.Unmarshal([]byte(`{"paidOn":"who knows"}`), &unreadable); err != nil {
		t.Fatalf("an unreadable timestamp must not fail the decode: %v", err)
	}
	if got := unreadable.PaidOnOr(fallback); !got.Equal(fallback) {
		t.Errorf("PaidOnOr = %v, want the fallback %v", got, fallback)
	}

	var good Transaction
	_ = json.Unmarshal([]byte(`{"paidOn":"2026-07-17 03:14:51.0"}`), &good)
	if got := good.PaidOnOr(fallback); got.Equal(fallback) {
		t.Error("PaidOnOr should return the real timestamp when it parsed")
	}
}

func TestAmount_AcceptsBothShapes(t *testing.T) {
	cases := []struct {
		in   string
		kobo int64
	}{
		{`"45000.00"`, 4500000}, // verify API: quoted
		{`45000.00`, 4500000},   // webhook docs: bare
		{`78000`, 7800000},
		{`"9,500.00"`, 950000}, // defensive
		{`0`, 0},
		{`null`, 0},
		{`""`, 0},
	}
	for _, c := range cases {
		var a Amount
		if err := json.Unmarshal([]byte(c.in), &a); err != nil {
			t.Errorf("Unmarshal(%s): %v", c.in, err)
			continue
		}
		if got := a.Kobo(); got != c.kobo {
			t.Errorf("Unmarshal(%s).Kobo() = %d, want %d", c.in, got, c.kobo)
		}
	}
}
