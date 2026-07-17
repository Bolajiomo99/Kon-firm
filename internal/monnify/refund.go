package monnify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Refund status values Monnify reports.
const (
	RefundPending   = "PENDING"
	RefundCompleted = "COMPLETED"
	RefundFailed    = "FAILED"
)

// Refund event types on webhooks.
const (
	EventSuccessfulRefund = "SUCCESSFUL_REFUND"
	EventFailedRefund     = "FAILED_REFUND"
)

// MinRefundKobo is Monnify's documented floor: ₦100.
const MinRefundKobo int64 = 10_000

// Field limits Monnify enforces. Exceeding them is rejected, so the caller is
// truncated here rather than discovering it at the API boundary.
const (
	MaxRefundReasonLen = 64
	MaxCustomerNoteLen = 16 // this one is genuinely tight
)

// RefundRequest initiates a refund.
type RefundRequest struct {
	// TransactionReference is Monnify's reference for the ORIGINAL payment,
	// not our order reference.
	TransactionReference string `json:"transactionReference"`
	// RefundReference is ours, and must be unique per refund attempt.
	RefundReference string  `json:"refundReference"`
	RefundAmount    float64 `json:"refundAmount"`
	RefundReason    string  `json:"refundReason"`
	// CustomerNote appears on the customer's bank credit alert. 16 characters.
	CustomerNote string `json:"customerNote"`
	// Optional: refund to a different account than the one that paid.
	DestinationAccountNumber   string `json:"destinationAccountNumber,omitempty"`
	DestinationAccountBankCode string `json:"destinationAccountBankCode,omitempty"`
}

// RefundResponse is what Monnify returns from an initiated refund.
type RefundResponse struct {
	RefundReference      string `json:"refundReference"`
	TransactionReference string `json:"transactionReference"`
	RefundStatus         string `json:"refundStatus"`
	RefundType           string `json:"refundType"`     // PARTIAL_REFUND | FULL_REFUND
	RefundStrategy       string `json:"refundStrategy"` // e.g. MERCHANT_WALLET
	Comment              string `json:"comment"`
	RefundAmount         Amount `json:"refundAmount"`
	RefundReason         string `json:"refundReason"`
	CompletedOn          Time   `json:"completedOn"`
	CreatedOn            Time   `json:"createdOn"`
}

// Completed reports whether the customer has actually been credited.
// PENDING is not a failure — Monnify may credit asynchronously — but it is
// also not done, and must not be reported to a customer as money returned.
func (r *RefundResponse) Completed() bool { return r.RefundStatus == RefundCompleted }
func (r *RefundResponse) Failed() bool    { return r.RefundStatus == RefundFailed }

// InitiateRefund asks Monnify to return money to the customer.
//
// Refunds are funded from the merchant wallet, so a sandbox wallet with no
// balance will reject them. That is a configuration answer, not a code one.
func (c *Client) InitiateRefund(ctx context.Context, in RefundRequest) (*RefundResponse, error) {
	switch {
	case in.TransactionReference == "":
		return nil, fmt.Errorf("monnify: TransactionReference is required")
	case in.RefundReference == "":
		return nil, fmt.Errorf("monnify: RefundReference is required and must be unique")
	case in.RefundAmount <= 0:
		return nil, fmt.Errorf("monnify: RefundAmount must be positive, got %v", in.RefundAmount)
	}

	in.RefundReason = truncateString(in.RefundReason, MaxRefundReasonLen)
	in.CustomerNote = truncateString(in.CustomerNote, MaxCustomerNoteLen)
	if in.CustomerNote == "" {
		in.CustomerNote = "Refund"
	}

	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.BaseURL+"/api/v1/refunds/initiate-refund", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	var out RefundResponse
	if err := c.do(req, &out); err != nil {
		return nil, fmt.Errorf("initiate refund: %w", err)
	}
	return &out, nil
}

// RefundEventData is the payload for SUCCESSFUL_REFUND / FAILED_REFUND.
type RefundEventData struct {
	RefundReference      string `json:"refundReference"`
	TransactionReference string `json:"transactionReference"`
	RefundStatus         string `json:"refundStatus"`
	RefundAmount         Amount `json:"refundAmount"`
	RefundReason         string `json:"refundReason"`
	CompletedOn          Time   `json:"completedOn"`
	Comment              string `json:"comment"`
}

// RefundData decodes a refund webhook payload.
func (e *WebhookEvent) RefundData() (*RefundEventData, error) {
	var d RefundEventData
	if err := json.Unmarshal(e.EventData, &d); err != nil {
		return nil, fmt.Errorf("monnify: malformed refund event data: %w", err)
	}
	return &d, nil
}

// truncateString bounds a field to Monnify's limit, cutting on a rune
// boundary so a multi-byte character is never sliced into invalid UTF-8.
func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	runes := []rune(s)
	for len(string(runes)) > max {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
