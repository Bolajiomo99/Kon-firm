package monnify

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SignatureHeader is the header Monnify puts the body hash in.
const SignatureHeader = "monnify-signature"

// WebhookSourceIP is Monnify's outbound webhook address, worth allowlisting
// at the edge. Treat it as defence in depth, never as authentication:
// source IPs are spoofable and Monnify may add egress addresses. The
// signature check below is the actual gate.
const WebhookSourceIP = "35.242.133.146"

// VerifySignature reports whether rawBody was signed with the merchant's
// client secret. rawBody MUST be the exact bytes received: any re-marshalling
// changes key order and whitespace, which changes the hash and fails a
// legitimate request.
//
// The comparison is constant-time to avoid leaking the expected signature
// through response timing.
func VerifySignature(secretKey string, rawBody []byte, gotSignature string) bool {
	if secretKey == "" || gotSignature == "" {
		return false
	}

	want := hmacSHA512Hex(secretKey, rawBody)

	// hmac.Equal is constant-time, but only over equal-length inputs. Decoding
	// both sides first normalises hex casing and keeps a malformed header from
	// short-circuiting the comparison.
	gotBytes, err := hex.DecodeString(gotSignature)
	if err != nil {
		return false
	}
	wantBytes, err := hex.DecodeString(want)
	if err != nil {
		return false
	}
	return hmac.Equal(gotBytes, wantBytes)
}

// hmacSHA512Hex computes the signature Monnify sends: a hex-encoded
// HMAC-SHA512 of the raw body, keyed with the merchant's client secret.
func hmacSHA512Hex(secretKey string, rawBody []byte) string {
	mac := hmac.New(sha512.New, []byte(secretKey))
	mac.Write(rawBody)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyRequest checks the signature on an inbound webhook request.
// It returns the raw body so the caller can decode it without re-reading.
func VerifyRequest(secretKey string, r *http.Request, rawBody []byte) error {
	sig := r.Header.Get(SignatureHeader)
	if sig == "" {
		return fmt.Errorf("monnify: missing %s header", SignatureHeader)
	}
	if !VerifySignature(secretKey, rawBody, sig) {
		return fmt.Errorf("monnify: webhook signature verification failed")
	}
	return nil
}

// Event types Monnify sends. SuccessfulTransaction is the one that moves money.
const (
	EventSuccessfulTransaction  = "SUCCESSFUL_TRANSACTION"
	EventFailedTransaction      = "FAILED_TRANSACTION"
	EventSuccessfulDisbursement = "SUCCESSFUL_DISBURSEMENT"
	EventFailedDisbursement     = "FAILED_DISBURSEMENT"
	EventSettlement             = "SETTLEMENT"
)

// WebhookEvent is the outer shape of a Monnify webhook notification.
type WebhookEvent struct {
	EventType string          `json:"eventType"`
	EventData json.RawMessage `json:"eventData"`
}

// Time wraps time.Time to cope with Monnify's timestamp formats.
//
// encoding/json only unmarshals RFC 3339 into a time.Time, but Monnify sends
// "17/11/2021 3:48:10 PM" on transaction webhooks. Decoding that into a plain
// time.Time fails the whole payload — which manifests as a 400 on every real
// notification while every hand-written test fixture passes, because fixtures
// get written in RFC 3339 out of habit.
//
// An unparseable timestamp is never fatal here. Rejecting a confirmed payment
// because we did not recognise its clock format would be absurd: the caller
// falls back to the time of receipt.
type Time struct{ time.Time }

// Layouts Monnify has been observed to send, most specific first.
var timeLayouts = []string{
	"02/01/2006 3:04:05 PM", // documented on transaction webhooks
	"02/01/2006 15:04:05",   // 24-hour variant
	time.RFC3339,            // used elsewhere in the API
	"2006-01-02T15:04:05.000Z0700",
	"2006-01-02 15:04:05",
}

func (t *Time) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	for _, layout := range timeLayouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			t.Time = parsed
			return nil
		}
	}
	// Deliberately not an error: leave the zero value and let the caller
	// substitute the receipt time. A payment is not less confirmed because we
	// could not read its timestamp.
	return nil
}

// Amount tolerates money arriving as either a JSON number or a JSON string.
//
// Monnify is not consistent about this: the webhook docs show `"amountPaid":
// 78000` while the transaction-status API returns amounts quoted. Committing
// to float64 makes the other shape a hard decode failure, which takes the
// whole payload down and reads as a bare 400 — an expensive way to learn that
// a field gained quotes.
//
// Being liberal in what we accept costs nothing here: the value is converted
// to integer kobo before any arithmetic touches it.
type Amount float64

func (a *Amount) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == `""` || s == "" {
		return nil
	}
	s = strings.Trim(s, `"`)           // number-as-string
	s = strings.ReplaceAll(s, ",", "") // defensive: "45,000.00"
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("monnify: cannot read amount %q: %w", s, err)
	}
	*a = Amount(f)
	return nil
}

func (a Amount) Float64() float64 { return float64(a) }

// Kobo converts to integer minor units, rounding to nearest. Truncation would
// silently lose a kobo, since float64 can hold 45000.00 as 44999.999...
func (a Amount) Kobo() int64 {
	if a < 0 {
		return 0
	}
	return int64(float64(a)*100 + 0.5)
}

// Customer tolerates the customer object being absent or null.
type Customer struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// TransactionEventData is the payload for transaction events.
//
// Every field is optional as far as decoding is concerned. Monnify sends
// different shapes for card, transfer, and offline payments, and a field we
// do not need must never be able to fail a payment we do.
type TransactionEventData struct {
	TransactionReference string `json:"transactionReference"`
	PaymentReference     string `json:"paymentReference"`
	PaidOn               Time   `json:"paidOn"`
	PaymentStatus        string `json:"paymentStatus"`
	PaymentDescription   string `json:"paymentDescription"`
	AmountPaid           Amount `json:"amountPaid"`
	TotalPayable         Amount `json:"totalPayable"`
	// SettlementAmount is what Monnify will actually settle to the merchant,
	// i.e. AmountPaid minus Monnify's fee.
	SettlementAmount Amount   `json:"settlementAmount"`
	Currency         string   `json:"currency"`
	PaymentMethod    string   `json:"paymentMethod"`
	Customer         Customer `json:"customer"`
}

// PaidOnOr returns the parsed payment time, or fallback when Monnify sent a
// timestamp we could not read.
func (d *TransactionEventData) PaidOnOr(fallback time.Time) time.Time {
	if d.PaidOn.IsZero() {
		return fallback
	}
	return d.PaidOn.Time
}

// ParseWebhook decodes a verified webhook body.
// Call VerifySignature first — this function does no authentication.
func ParseWebhook(rawBody []byte) (*WebhookEvent, error) {
	var ev WebhookEvent
	if err := json.Unmarshal(rawBody, &ev); err != nil {
		return nil, fmt.Errorf("monnify: malformed webhook body: %w", err)
	}
	if ev.EventType == "" {
		return nil, fmt.Errorf("monnify: webhook body has no eventType")
	}
	return &ev, nil
}

// TransactionData decodes the event payload as a transaction event.
func (e *WebhookEvent) TransactionData() (*TransactionEventData, error) {
	var d TransactionEventData
	if err := json.Unmarshal(e.EventData, &d); err != nil {
		return nil, fmt.Errorf("monnify: malformed transaction event data: %w", err)
	}
	return &d, nil
}

// TransactionStatus is the server-side view of a transaction.
type TransactionStatus struct {
	TransactionReference string `json:"transactionReference"`
	PaymentReference     string `json:"paymentReference"`
	PaymentStatus        string `json:"paymentStatus"`
	AmountPaid           string `json:"amountPaid"`
	TotalPayable         string `json:"totalPayable"`
	PaidOn               string `json:"paidOn"`
	PaymentMethod        string `json:"paymentMethod"`
	Currency             string `json:"currency"`
}

// Paid reports whether the transaction is settled and fully paid.
func (t *TransactionStatus) Paid() bool { return t.PaymentStatus == "PAID" }
