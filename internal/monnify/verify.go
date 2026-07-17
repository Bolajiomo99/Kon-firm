package monnify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Payment status values Monnify reports.
//
// OVERPAID and PARTIALLY_PAID only occur when the contract is configured to
// accept them; otherwise Monnify rejects the payment before it reaches us.
const (
	StatusPaid          = "PAID"
	StatusOverpaid      = "OVERPAID"
	StatusPartiallyPaid = "PARTIALLY_PAID"
	StatusPending       = "PENDING"
	StatusFailed        = "FAILED"
	StatusExpired       = "EXPIRED"
	StatusCancelled     = "USER_CANCELLED"
)

// Transaction is Monnify's server-side view of a transaction.
//
// This is the authoritative answer to "was this actually paid?" — asked of
// Monnify directly, over an authenticated channel, rather than inferred from
// a push we might never receive.
//
// These field names are taken from an actual sandbox response, not from the
// documentation. An earlier version of this struct was written from the docs
// and invented completedOn, payableAmount, fee, currencyCode and
// customerName — none of which Monnify sends. Absent JSON keys do not error;
// they leave zero values, so the bug surfaced as a paidAt of 0001-01-01
// rather than as anything that looked like a failure.
//
// Verified response shape:
//
//	{"transactionReference":"MNFY|41|...","paymentReference":"KF-...",
//	 "amountPaid":"45000.00","totalPayable":"45000.00",
//	 "settlementAmount":"44990.00","paidOn":"2026-07-17 03:14:51.0",
//	 "paymentStatus":"PAID","paymentMethod":"ACCOUNT_TRANSFER",
//	 "currency":"NGN","customer":{"name":"...","email":"..."}}
type Transaction struct {
	TransactionReference string `json:"transactionReference"`
	PaymentReference     string `json:"paymentReference"`
	PaymentStatus        string `json:"paymentStatus"`
	PaymentMethod        string `json:"paymentMethod"`
	PaymentDescription   string `json:"paymentDescription"`
	Currency             string `json:"currency"`
	PaidOn               Time   `json:"paidOn"`
	// Amounts come back quoted here ("45000.00"), so they go through the
	// tolerant type rather than float64.
	AmountPaid   Amount `json:"amountPaid"`
	TotalPayable Amount `json:"totalPayable"`
	// SettlementAmount is AmountPaid minus Monnify's fee — what the merchant
	// actually receives.
	SettlementAmount Amount   `json:"settlementAmount"`
	Customer         Customer `json:"customer"`
}

// FeeKobo is what Monnify keeps: the gap between what the customer paid and
// what gets settled to the merchant.
func (t *Transaction) FeeKobo() int64 {
	if t.SettlementAmount == 0 {
		return 0 // not reported; do not invent a fee
	}
	return t.AmountPaid.Kobo() - t.SettlementAmount.Kobo()
}

// PaidOnOr returns the payment time, or fallback if Monnify sent a timestamp
// we could not read.
func (t *Transaction) PaidOnOr(fallback time.Time) time.Time {
	if t.PaidOn.IsZero() {
		return fallback
	}
	return t.PaidOn.Time
}

// Paid reports whether money actually landed. OVERPAID counts: the customer
// paid at least the bill, and refusing them their goods over a surplus would
// be perverse. PARTIALLY_PAID does not.
func (t *Transaction) Paid() bool {
	return t.PaymentStatus == StatusPaid || t.PaymentStatus == StatusOverpaid
}

// VerifyByTransactionReference asks Monnify the status of a transaction.
//
// Monnify's own guidance is to verify server-side before giving value, rather
// than trusting a notification alone. Webhooks are a fast path, not a
// guarantee: they can be dropped, delayed, or rejected by a bug on our side.
// This is the path that does not depend on anyone else's delivery succeeding.
func (c *Client) VerifyByTransactionReference(ctx context.Context, transactionRef string) (*Transaction, error) {
	if transactionRef == "" {
		return nil, fmt.Errorf("monnify: transaction reference is required")
	}

	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}

	// References contain pipes ("MNFY|41|..."), which must be escaped.
	endpoint := c.cfg.BaseURL + "/api/v2/transactions/" + url.PathEscape(transactionRef)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	var out Transaction
	if err := c.do(req, &out); err != nil {
		return nil, fmt.Errorf("verify transaction %q: %w", transactionRef, err)
	}
	return &out, nil
}

// VerifyByPaymentReference asks Monnify about a transaction using our own
// reference, for when Monnify's is unknown — an init that returned but was
// never persisted, say.
func (c *Client) VerifyByPaymentReference(ctx context.Context, paymentRef string) (*Transaction, error) {
	if paymentRef == "" {
		return nil, fmt.Errorf("monnify: payment reference is required")
	}

	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}

	endpoint := c.cfg.BaseURL + "/api/v1/merchant/transactions/query?paymentReference=" +
		url.QueryEscape(paymentRef)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	var out Transaction
	if err := c.do(req, &out); err != nil {
		return nil, fmt.Errorf("query transaction %q: %w", paymentRef, err)
	}
	return &out, nil
}

// IsNotFound reports whether err is Monnify saying it has never heard of the
// transaction — expected when a customer abandons checkout, and not a fault.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusNotFound ||
		strings.Contains(strings.ToLower(apiErr.Message), "not found")
}
