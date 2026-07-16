// Package monnify is a client for the Monnify payments API.
//
// Base URLs:
//   - Sandbox:    https://sandbox.monnify.com
//   - Production: https://api.monnify.com
//
// Test credentials are prefixed MK_TEST_, live ones MK_PROD_. Pointing test
// keys at the production base URL (or vice versa) is the most common
// integration error, so NewClient rejects that combination outright.
package monnify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	SandboxBaseURL    = "https://sandbox.monnify.com"
	ProductionBaseURL = "https://api.monnify.com"

	// Monnify access tokens live for an hour. We refresh early so an
	// in-flight request never races the expiry.
	tokenRefreshSkew = 5 * time.Minute
)

// Config holds the credentials from the Monnify dashboard, under
// Developer > API Keys & Contracts.
type Config struct {
	APIKey       string
	SecretKey    string
	ContractCode string
	BaseURL      string
}

// Client is safe for concurrent use. The zero value is not usable; call NewClient.
type Client struct {
	cfg  Config
	http *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

func NewClient(cfg Config) (*Client, error) {
	switch {
	case cfg.APIKey == "":
		return nil, fmt.Errorf("monnify: APIKey is required")
	case cfg.SecretKey == "":
		return nil, fmt.Errorf("monnify: SecretKey is required")
	case cfg.ContractCode == "":
		return nil, fmt.Errorf("monnify: ContractCode is required")
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = SandboxBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	// Guard the classic mismatch before it costs anyone an afternoon.
	isTestKey := strings.HasPrefix(cfg.APIKey, "MK_TEST_")
	isProdKey := strings.HasPrefix(cfg.APIKey, "MK_PROD_")
	if isTestKey && cfg.BaseURL == ProductionBaseURL {
		return nil, fmt.Errorf("monnify: refusing to use MK_TEST_ key against the production base URL")
	}
	if isProdKey && cfg.BaseURL == SandboxBaseURL {
		return nil, fmt.Errorf("monnify: refusing to use MK_PROD_ key against the sandbox base URL")
	}

	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// envelope is the wrapper Monnify puts around every response body.
type envelope struct {
	RequestSuccessful bool            `json:"requestSuccessful"`
	ResponseMessage   string          `json:"responseMessage"`
	ResponseCode      string          `json:"responseCode"`
	ResponseBody      json.RawMessage `json:"responseBody"`
}

// APIError is returned when Monnify reports a business-level failure.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("monnify: api error (http %d, code %s): %s", e.StatusCode, e.Code, e.Message)
}

// token returns a cached access token, fetching a new one if needed.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenExp) {
		return c.token, nil
	}

	basic := base64.StdEncoding.EncodeToString([]byte(c.cfg.APIKey + ":" + c.cfg.SecretKey))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/api/v1/auth/login", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Basic "+basic)
	req.Header.Set("Content-Type", "application/json")

	var body struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int64  `json:"expiresIn"`
	}
	if err := c.do(req, &body); err != nil {
		return "", fmt.Errorf("authenticate: %w", err)
	}
	if body.AccessToken == "" {
		return "", fmt.Errorf("monnify: login succeeded but returned no access token")
	}

	ttl := time.Duration(body.ExpiresIn) * time.Second
	if ttl <= tokenRefreshSkew {
		ttl = tokenRefreshSkew * 2 // defensive: never cache into the past
	}

	c.token = body.AccessToken
	c.tokenExp = time.Now().Add(ttl - tokenRefreshSkew)
	return c.token, nil
}

// do executes req, unwraps the Monnify envelope, and decodes responseBody into out.
func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("monnify: could not decode response (http %d): %s", resp.StatusCode, truncate(raw, 200))
	}

	if !env.RequestSuccessful {
		return &APIError{StatusCode: resp.StatusCode, Code: env.ResponseCode, Message: env.ResponseMessage}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.ResponseBody, out)
}

// InitTransactionRequest is the payload for initialising a one-time payment.
type InitTransactionRequest struct {
	Amount        float64  `json:"amount"`
	CustomerName  string   `json:"customerName"`
	CustomerEmail string   `json:"customerEmail"`
	// PaymentReference must be unique per transaction; reusing one is rejected.
	PaymentReference   string   `json:"paymentReference"`
	PaymentDescription string   `json:"paymentDescription"`
	CurrencyCode       string   `json:"currencyCode"`
	ContractCode       string   `json:"contractCode"`
	RedirectURL        string   `json:"redirectUrl"`
	PaymentMethods     []string `json:"paymentMethods,omitempty"`
}

// InitTransactionResponse carries the checkout URL to send the customer to.
// The checkout URL is valid for 40 minutes.
type InitTransactionResponse struct {
	TransactionReference string `json:"transactionReference"`
	PaymentReference     string `json:"paymentReference"`
	MerchantName         string `json:"merchantName"`
	CheckoutURL          string `json:"checkoutUrl"`
	EnabledPaymentMethod []string `json:"enabledPaymentMethod"`
}

// InitTransaction creates a transaction and returns its checkout URL.
func (c *Client) InitTransaction(ctx context.Context, in InitTransactionRequest) (*InitTransactionResponse, error) {
	if in.PaymentReference == "" {
		return nil, fmt.Errorf("monnify: PaymentReference is required and must be unique")
	}
	if in.Amount <= 0 {
		return nil, fmt.Errorf("monnify: Amount must be positive, got %v", in.Amount)
	}

	if in.ContractCode == "" {
		in.ContractCode = c.cfg.ContractCode
	}
	if in.CurrencyCode == "" {
		in.CurrencyCode = "NGN"
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
		c.cfg.BaseURL+"/api/v1/merchant/transactions/init-transaction", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	var out InitTransactionResponse
	if err := c.do(req, &out); err != nil {
		return nil, fmt.Errorf("init transaction: %w", err)
	}

	// The docs are explicit: confirm what came back matches what we sent.
	// A mismatch means the response is not describing our order.
	if out.PaymentReference != in.PaymentReference {
		return nil, fmt.Errorf("monnify: payment reference mismatch: sent %q, received %q",
			in.PaymentReference, out.PaymentReference)
	}

	return &out, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
