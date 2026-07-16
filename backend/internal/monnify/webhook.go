package monnify

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
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
	EventSuccessfulTransaction = "SUCCESSFUL_TRANSACTION"
	EventFailedTransaction     = "FAILED_TRANSACTION"
	EventSuccessfulDisbursement = "SUCCESSFUL_DISBURSEMENT"
	EventFailedDisbursement     = "FAILED_DISBURSEMENT"
	EventSettlement             = "SETTLEMENT"
)

// WebhookEvent is the outer shape of a Monnify webhook notification.
type WebhookEvent struct {
	EventType string          `json:"eventType"`
	EventData json.RawMessage `json:"eventData"`
}

// TransactionEventData is the payload for transaction events.
type TransactionEventData struct {
	TransactionReference string    `json:"transactionReference"`
	PaymentReference     string    `json:"paymentReference"`
	PaidOn               time.Time `json:"paidOn"`
	PaymentStatus        string    `json:"paymentStatus"`
	PaymentDescription   string    `json:"paymentDescription"`
	AmountPaid           float64   `json:"amountPaid"`
	TotalPayable         float64   `json:"totalPayable"`
	Currency             string    `json:"currency"`
	PaymentMethod        string    `json:"paymentMethod"`
	Customer             struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"customer"`
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
	TransactionReference string  `json:"transactionReference"`
	PaymentReference     string  `json:"paymentReference"`
	PaymentStatus        string  `json:"paymentStatus"`
	AmountPaid           string  `json:"amountPaid"`
	TotalPayable         string  `json:"totalPayable"`
	PaidOn               string  `json:"paidOn"`
	PaymentMethod        string  `json:"paymentMethod"`
	Currency             string  `json:"currency"`
}

// Paid reports whether the transaction is settled and fully paid.
func (t *TransactionStatus) Paid() bool { return t.PaymentStatus == "PAID" }
