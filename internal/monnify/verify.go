package monnify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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
type Transaction struct {
	TransactionReference string `json:"transactionReference"`
	PaymentReference     string `json:"paymentReference"`
	PaymentStatus        string `json:"paymentStatus"`
	PaymentMethod        string `json:"paymentMethod"`
	CurrencyCode         string `json:"currencyCode"`
	CustomerName         string `json:"customerName"`
	CustomerEmail        string `json:"customerEmail"`
	Completed            bool   `json:"completed"`
	CompletedOn          Time   `json:"completedOn"`
	CreatedOn            Time   `json:"createdOn"`
	// Amounts arrive quoted from this endpoint but unquoted on webhooks, so
	// they go through the tolerant type.
	Amount        Amount `json:"amount"`
	AmountPaid    Amount `json:"amountPaid"`
	PayableAmount Amount `json:"payableAmount"`
	Fee           Amount `json:"fee"`
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
