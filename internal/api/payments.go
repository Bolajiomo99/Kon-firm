package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Bolajiomo99/Kon-firm/internal/monnify"
	"github.com/Bolajiomo99/Kon-firm/internal/store"
)

// maxWebhookBody caps how much we will read from an inbound webhook. Without
// it, a hostile POST could stream indefinitely into memory.
const maxWebhookBody = 1 << 20 // 1 MiB

type checkoutLine struct {
	ProductID int64 `json:"productId"`
	Quantity  int   `json:"quantity"`
}

// checkoutRequest is what the browser may send. Note what is absent: prices.
// The client states intent (which products, how many); the server decides cost.
type checkoutRequest struct {
	CustomerName  string         `json:"customerName"`
	CustomerEmail string         `json:"customerEmail"`
	Channel       string         `json:"channel"` // "online" | "pos"
	Items         []checkoutLine `json:"items"`
}

type checkoutResponse struct {
	Reference   string `json:"reference"`
	CheckoutURL string `json:"checkoutUrl"`
	TotalKobo   int64  `json:"totalKobo"`
	TotalNaira  string `json:"totalNaira"`
}

// newReference mints a unique paymentReference. Monnify rejects a reused one,
// so this must not collide: 8 random bytes plus a timestamp is ample.
func newReference() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("KF-%d-%s", time.Now().Unix(), hex.EncodeToString(b)), nil
}

// koboToNaira converts minor units to the decimal string Monnify expects.
// Integer division and modulo avoid ever putting money through a float.
func koboToNaira(kobo int64) string {
	return fmt.Sprintf("%d.%02d", kobo/100, kobo%100)
}

func (s *Server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	var req checkoutRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.CustomerName = strings.TrimSpace(req.CustomerName)
	req.CustomerEmail = strings.TrimSpace(req.CustomerEmail)

	if req.CustomerName == "" {
		writeError(w, http.StatusBadRequest, "customerName is required")
		return
	}
	if !strings.Contains(req.CustomerEmail, "@") {
		writeError(w, http.StatusBadRequest, "a valid customerEmail is required")
		return
	}
	if len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "cart is empty")
		return
	}
	if req.Channel != "pos" {
		req.Channel = "online"
	}

	lines := make([]store.CreateOrderLine, 0, len(req.Items))
	for _, it := range req.Items {
		lines = append(lines, store.CreateOrderLine{ProductID: it.ProductID, Quantity: it.Quantity})
	}

	ref, err := newReference()
	if err != nil {
		s.log.Error("mint reference", "err", err)
		writeError(w, http.StatusInternalServerError, "could not start checkout")
		return
	}

	// Price the cart server-side. This is also where stock is validated.
	order, err := s.store.CreateOrder(r.Context(), ref, req.CustomerName, req.CustomerEmail, req.Channel, lines)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "one or more products are unavailable")
			return
		}
		// Stock shortfalls are the customer's business; surface them.
		if strings.Contains(err.Error(), "insufficient stock") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		s.log.Error("create order", "err", err)
		writeError(w, http.StatusInternalServerError, "could not create order")
		return
	}

	init, err := s.monnify.InitTransaction(r.Context(), monnify.InitTransactionRequest{
		Amount:             float64(order.TotalKobo) / 100,
		CustomerName:       order.CustomerName,
		CustomerEmail:      order.CustomerEmail,
		PaymentReference:   order.Reference,
		PaymentDescription: "Kon-firm order " + order.Reference,
		CurrencyCode:       "NGN",
		ContractCode:       s.cfg.MonnifyContractCode,
		RedirectURL:        s.cfg.RedirectURL,
	})
	if err != nil {
		s.log.Error("monnify init transaction", "err", err, "ref", order.Reference)
		writeError(w, http.StatusBadGateway, "payment provider could not start this transaction")
		return
	}

	if err := s.store.AttachCheckout(r.Context(), order.Reference, init.TransactionReference, init.CheckoutURL); err != nil {
		s.log.Error("attach checkout", "err", err, "ref", order.Reference)
		writeError(w, http.StatusInternalServerError, "could not save checkout details")
		return
	}

	writeJSON(w, http.StatusOK, checkoutResponse{
		Reference:   order.Reference,
		CheckoutURL: init.CheckoutURL,
		TotalKobo:   order.TotalKobo,
		TotalNaira:  koboToNaira(order.TotalKobo),
	})
}

// handleMonnifyWebhook is the only path that may mark an order paid.
//
// Order of operations matters:
//  1. Read the raw bytes — the signature covers exactly these, so they must
//     not be re-marshalled before verification.
//  2. Verify the signature. Everything before this point is untrusted input.
//  3. Apply idempotently. A replay is acknowledged, not re-applied.
func (s *Server) handleMonnifyWebhook(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read body")
		return
	}

	if err := monnify.VerifyRequest(s.cfg.MonnifySecretKey, r, rawBody); err != nil {
		// Do not echo the reason: a precise error tells a forger what to fix.
		s.log.Warn("rejected unsigned webhook", "err", err, "remote", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	event, err := monnify.ParseWebhook(rawBody)
	if err != nil {
		// Log the body when a *signed* payload fails to parse. It came from
		// Monnify, so the shape is the story: without this, a schema mismatch
		// looks like a bare 400 and there is nothing to debug from. Safe to
		// log — the signature already proved provenance, and a webhook body
		// carries no secret material.
		s.log.Error("parse webhook", "err", err, "body", truncateBody(rawBody))
		writeError(w, http.StatusBadRequest, "malformed webhook")
		return
	}

	switch event.EventType {
	case monnify.EventSuccessfulTransaction, monnify.EventFailedTransaction:
		// handled below
	default:
		// Acknowledge events we do not act on, so Monnify stops retrying them.
		s.log.Info("ignoring webhook event", "type", event.EventType)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	data, err := event.TransactionData()
	if err != nil {
		s.log.Error("decode transaction event", "err", err, "body", truncateBody(rawBody))
		writeError(w, http.StatusBadRequest, "malformed transaction data")
		return
	}

	paidKobo := nairaToKobo(data.AmountPaid)
	payableKobo := nairaToKobo(data.TotalPayable)

	// An order settles only if Monnify says PAID *and* the money actually
	// covers the bill. Trusting paymentStatus alone would let a short transfer
	// mark an order paid — and both values here come from the signed payload,
	// so neither can be forged.
	success := event.EventType == monnify.EventSuccessfulTransaction &&
		data.PaymentStatus == "PAID" &&
		paidKobo >= payableKobo

	if event.EventType == monnify.EventSuccessfulTransaction && data.PaymentStatus == "PAID" && !success {
		s.log.Warn("underpayment refused",
			"ref", data.PaymentReference, "paid_kobo", paidKobo, "payable_kobo", payableKobo)
	}

	order, err := s.store.ApplyWebhook(r.Context(), store.PaymentResult{
		TransactionRef: data.TransactionReference,
		PaymentRef:     data.PaymentReference,
		EventType:      event.EventType,
		AmountPaidKobo: paidKobo,
		PaymentMethod:  data.PaymentMethod,
		// Monnify's timestamp format varies; fall back to receipt time rather
		// than storing a zero date.
		PaidAt:     data.PaidOnOr(time.Now().UTC()),
		Success:    success,
		RawPayload: rawBody,
	})

	switch {
	case errors.Is(err, store.ErrAlreadyProcessed):
		// The ledger did its job. 200 so Monnify stops redelivering.
		s.log.Info("duplicate webhook ignored", "ref", data.PaymentReference, "type", event.EventType)
		writeJSON(w, http.StatusOK, map[string]string{"status": "already processed"})
		return

	case errors.Is(err, store.ErrNotFound):
		// Unknown or already-settled reference. 200 prevents an endless retry
		// loop over something we will never be able to apply.
		s.log.Warn("webhook for unknown or settled order", "ref", data.PaymentReference)
		writeJSON(w, http.StatusOK, map[string]string{"status": "no pending order"})
		return

	case err != nil:
		// A genuine failure: 500 asks Monnify to retry, which is what we want.
		s.log.Error("apply webhook", "err", err, "ref", data.PaymentReference)
		writeError(w, http.StatusInternalServerError, "could not process notification")
		return
	}

	s.log.Info("order settled", "ref", order.Reference, "status", order.Status,
		"amount_kobo", order.TotalKobo, "method", order.PaymentMethod)
	writeJSON(w, http.StatusOK, map[string]string{"status": "processed"})
}

// nairaToKobo converts Monnify's decimal amount to minor units.
//
// The +0.5 rounds to nearest rather than truncating: float64 can hold 5000.00
// as 4999.999..., and truncation would silently lose a kobo.
func nairaToKobo(naira float64) int64 {
	if naira < 0 {
		return 0
	}
	return int64(naira*100 + 0.5)
}

func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("reference")
	order, err := s.store.OrderByReference(r.Context(), ref)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "order not found")
		return
	}
	if err != nil {
		s.log.Error("get order", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load order")
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (s *Server) handleProductByBarcode(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.ProductByBarcode(r.Context(), r.PathValue("barcode"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no product with that barcode")
		return
	}
	if err != nil {
		s.log.Error("barcode lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// truncateBody bounds a body for logging. Webhook payloads are small, but a
// log line is not the place to discover otherwise.
func truncateBody(b []byte) string {
	const max = 2000
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}
