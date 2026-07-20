package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Bolajiomo99/Kon-firm/internal/auth"
	"github.com/Bolajiomo99/Kon-firm/internal/email"
	"github.com/Bolajiomo99/Kon-firm/internal/events"
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
	VoucherCode   string         `json:"voucherCode"`

	// Delivery. Required for online orders; a POS sale is handed over the
	// counter, so there is nothing to deliver.
	DeliveryPhone   string   `json:"deliveryPhone"`
	DeliveryAddress string   `json:"deliveryAddress"`
	DeliveryCity    string   `json:"deliveryCity"`
	DeliveryState   string   `json:"deliveryState"`
	DeliveryLat     *float64 `json:"deliveryLat"`
	DeliveryLng     *float64 `json:"deliveryLng"`
}

type checkoutResponse struct {
	Reference   string      `json:"reference"`
	CheckoutURL string      `json:"checkoutUrl"`
	TotalKobo   int64       `json:"totalKobo"`
	TotalNaira  string      `json:"totalNaira"`
	Quote       store.Quote `json:"quote"`
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

	// An online order has to go somewhere. A POS sale does not.
	if req.Channel != "pos" {
		req.DeliveryAddress = strings.TrimSpace(req.DeliveryAddress)
		req.DeliveryCity = strings.TrimSpace(req.DeliveryCity)
		req.DeliveryState = strings.TrimSpace(req.DeliveryState)
		if req.DeliveryAddress == "" {
			writeError(w, http.StatusBadRequest, "a delivery address is required")
			return
		}
		if req.DeliveryState == "" {
			writeError(w, http.StatusBadRequest, "please choose your state")
			return
		}
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

	// A signed-in shopper gets the order filed against their account; a guest
	// still gets to buy. The identity comes from the session, never from the
	// request body — a client-supplied user id would let anyone file an order
	// into someone else's history.
	buyer := userFrom(r.Context())
	if buyer != nil && buyer.Role == auth.RoleCustomer {
		if req.CustomerName == "" {
			req.CustomerName = buyer.Name
		}
		if req.CustomerEmail == "" {
			req.CustomerEmail = buyer.Email
		}
	}

	// Price the basket server-side, through the same path the quote endpoint
	// uses, so the total shown can never drift from the total charged.
	subtotal, err := s.store.PriceBasket(r.Context(), lines)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "one or more products are unavailable")
			return
		}
		// A basket outside sane bounds is the caller's mistake, not ours.
		if errors.Is(err, store.ErrUnreasonableBasket) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.log.Error("checkout: price basket", "err", err)
		writeError(w, http.StatusInternalServerError, "could not price your basket")
		return
	}

	// The voucher is re-validated here, never trusted from the request. A
	// browser that could name its own discount could name any discount.
	var discount int64
	var voucherCode string
	if c := strings.TrimSpace(req.VoucherCode); c != "" {
		v, verr := s.store.VoucherByCode(r.Context(), c, subtotal)
		if verr == nil {
			discount = v.DiscountFor(subtotal)
			voucherCode = v.Code
		}
		// A code that has expired between quoting and paying is simply not
		// applied. Failing the checkout over it would lose the sale.
	}

	fee := store.DeliveryFee(subtotal-discount, req.DeliveryState)
	if req.Channel == "pos" {
		fee = 0 // handed over the counter
	}
	quote := store.BuildQuote(subtotal, discount, fee, voucherCode)

	order, err := s.store.CreateOrder(r.Context(), ref, req.CustomerName, req.CustomerEmail, req.Channel,
		lines, quote, store.Delivery{
			Phone: req.DeliveryPhone, Address: req.DeliveryAddress,
			City: req.DeliveryCity, State: req.DeliveryState,
			Lat: req.DeliveryLat, Lng: req.DeliveryLng,
		})
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

	// File the order against the buyer's account, if there is one. Best-effort
	// on purpose: an order that is paid for but missing from a history page is
	// a nuisance, whereas failing the checkout over it would cost a sale.
	if buyer != nil && buyer.Role == auth.RoleCustomer {
		if err := s.store.AttachOrderToUser(r.Context(), order.Reference, buyer.ID); err != nil {
			s.log.Error("attach order to user", "err", err, "ref", order.Reference, "user_id", buyer.ID)
		}
	}

	// Count the redemption once the order exists. Doing it at quote time would
	// burn a limited code every time someone typed it to look.
	if voucherCode != "" {
		if err := s.store.RedeemVoucher(r.Context(), voucherCode); err != nil {
			s.log.Error("redeem voucher", "err", err, "code", voucherCode)
		}
	}

	// A new order is dashboard news even before it is paid.
	s.events.Publish(events.TopicAdmin, events.Event{
		Type: events.TypeOrderCreated, Ref: order.Reference, Data: order,
	})

	writeJSON(w, http.StatusOK, checkoutResponse{
		Reference:   order.Reference,
		CheckoutURL: init.CheckoutURL,
		TotalKobo:   order.TotalKobo,
		TotalNaira:  koboToNaira(order.TotalKobo),
		Quote:       quote,
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

	// Route by what the payload IS, not only by what the event is called.
	//
	// Monnify publishes an event named "Completed Offline Payments" for cash
	// taken at a Moniepoint agent, but does not document the literal eventType
	// string. An allow-list of names we guessed would silently drop it: the
	// customer pays cash, Monnify tells us, we answer 200 "ignored", and their
	// order stays pending forever with no error anywhere.
	//
	// That is exactly how the paidOn bug behaved — an assumption about their
	// payload that looked fine until real traffic arrived. So the rule here is
	//: a signed webhook carrying a PAID transaction settles the matching
	// order, whatever the event is called.
	switch {
	case strings.Contains(event.EventType, "REFUND"):
		s.applyRefundEvent(w, r, event, rawBody)
		return

	case event.EventType == monnify.EventSuccessfulTransaction,
		event.EventType == monnify.EventFailedTransaction:
		// Known, handled below.

	default:
		// Unknown name. Decide by the body: does it describe a real payment
		// against an order of ours?
		if d, err := event.TransactionData(); err == nil &&
			d.PaymentReference != "" && d.PaymentStatus == "PAID" {
			s.log.Info("settling an unrecognised event that carries a paid transaction",
				"type", event.EventType, "ref", d.PaymentReference)
			break // fall through to the settlement path below
		}
		// Genuinely nothing to do — acknowledge so Monnify stops retrying.
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

	paidKobo := data.AmountPaid.Kobo()
	payableKobo := data.TotalPayable.Kobo()

	// An order settles only if Monnify says PAID *and* the money actually
	// covers the bill. Trusting paymentStatus alone would let a short transfer
	// mark an order paid — and both values here come from the signed payload,
	// so neither can be forged.
	// A payment counts when Monnify says PAID and the money covers the bill —
	// not when the event happens to carry a name we recognise. An explicit
	// FAILED event is the one case that overrides a PAID status.
	success := event.EventType != monnify.EventFailedTransaction &&
		data.PaymentStatus == "PAID" &&
		paidKobo >= payableKobo

	if data.PaymentStatus == "PAID" && paidKobo < payableKobo {
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

	// Push to any open dashboard and to the customer's receipt page, so both
	// update without a refresh.
	evType := events.TypeOrderPaid
	if order.Status != "paid" {
		evType = events.TypeOrderFailed
	}
	s.events.PublishOrder(evType, order.Reference, order)

	if order.Status == "paid" {
		s.sendReceipt(r.Context(), order)
	}

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

	// If the order is still pending, ask Monnify directly rather than waiting
	// on a webhook that may never arrive. Monnify's own guidance is to verify
	// server-side before giving value; a webhook is a fast path, not a
	// guarantee. This is what stops a dropped notification from stranding a
	// customer who has genuinely paid.
	if order.Status == "pending" && order.TransactionRef != "" {
		if settled := s.reconcile(r.Context(), order); settled != nil {
			order = settled
		}
	}

	writeJSON(w, http.StatusOK, order)
}

// reconcile asks Monnify about a pending order and settles it if it was paid.
// Returns nil when nothing changed, so the caller keeps what it already had.
//
// Reconciliation goes through the same ApplyWebhook path as a notification,
// so the same UNIQUE constraint applies: if a webhook lands at the same
// moment, exactly one of them credits the order.
func (s *Server) reconcile(ctx context.Context, order *store.Order) *store.Order {
	tx, err := s.monnify.VerifyByTransactionReference(ctx, order.TransactionRef)
	if err != nil {
		if !monnify.IsNotFound(err) {
			s.log.Warn("reconcile failed", "ref", order.Reference, "err", err)
		}
		return nil
	}

	if !tx.Paid() {
		return nil // genuinely not paid yet; leave it pending
	}

	paidKobo := tx.AmountPaid.Kobo()
	if paidKobo < order.TotalKobo {
		s.log.Warn("reconcile: underpayment refused",
			"ref", order.Reference, "paid_kobo", paidKobo, "owed_kobo", order.TotalKobo)
		return nil
	}

	// Distinct event type from a webhook's, so reconciliation and a late
	// notification each get their own ledger row while the order itself is
	// still only ever credited once.
	raw, _ := json.Marshal(tx)
	settled, err := s.store.ApplyWebhook(ctx, store.PaymentResult{
		TransactionRef: tx.TransactionReference,
		PaymentRef:     order.Reference,
		EventType:      "RECONCILED_VERIFICATION",
		AmountPaidKobo: paidKobo,
		PaymentMethod:  tx.PaymentMethod,
		PaidAt:         tx.PaidOnOr(time.Now().UTC()),
		Success:        true,
		RawPayload:     raw,
	})

	switch {
	case errors.Is(err, store.ErrAlreadyProcessed), errors.Is(err, store.ErrNotFound):
		// A webhook beat us to it. Re-read rather than reporting stale state.
		fresh, err := s.store.OrderByReference(ctx, order.Reference)
		if err != nil {
			return nil
		}
		return fresh
	case err != nil:
		s.log.Error("reconcile: settle failed", "ref", order.Reference, "err", err)
		return nil
	}

	s.log.Info("order settled by reconciliation — no usable webhook arrived",
		"ref", settled.Reference, "amount_kobo", paidKobo)
	s.events.PublishOrder(events.TypeOrderPaid, settled.Reference, settled)
	s.sendReceipt(ctx, settled)
	return settled
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

// sendReceipt emails a confirmed order to its customer.
//
// Called from both settlement paths — the webhook and reconciliation — because
// a customer who was confirmed by the fallback needs their receipt just as
// much as one confirmed by a notification. Arguably more: the fallback exists
// precisely because their browser never came back.
//
// Fire-and-forget by construction. The money has already moved; a mail server
// is not permitted to fail a payment, so nothing here returns an error.
func (s *Server) sendReceipt(ctx context.Context, order *store.Order) {
	// Re-read for the line items and delivery address. Worth one query: a
	// receipt without what was bought is not a receipt.
	full, err := s.store.OrderByReference(ctx, order.Reference)
	if err != nil {
		s.log.Error("receipt: could not load order", "err", err, "ref", order.Reference)
		return
	}

	lines := make([]email.ReceiptLine, 0, len(full.Items))
	for _, it := range full.Items {
		lines = append(lines, email.ReceiptLine{
			Name:     it.ProductName,
			Quantity: it.Quantity,
			Amount:   naira(it.UnitPriceKobo * int64(it.Quantity)),
		})
	}

	addr := strings.TrimSpace(strings.Join([]string{
		full.DeliveryAddress, full.DeliveryCity, full.DeliveryState,
	}, ", "))
	addr = strings.Trim(addr, ", ")

	q := full.Quote
	r := email.Receipt{
		CustomerName:  full.CustomerName,
		Email:         full.CustomerEmail,
		Reference:     full.Reference,
		MonnifyRef:    full.TransactionRef,
		PaymentMethod: prettyMethod(full.PaymentMethod),
		Lines:         lines,
		Subtotal:      naira(q.SubtotalKobo),
		Delivery:      naira(q.DeliveryFeeKobo),
		FreeDelivery:  q.DeliveryFeeKobo == 0,
		Total:         naira(full.TotalKobo),
		VAT:           naira(q.VATKobo),
		VATRate:       fmt.Sprintf("%g%%", float64(q.VATRateBP)/100),
		VoucherCode:   q.VoucherCode,
		Address:       addr,
	}
	if q.DiscountKobo > 0 {
		r.Discount = naira(q.DiscountKobo)
	}
	if full.PaidAt != nil {
		r.PaidAt = *full.PaidAt
	}
	if s.cfg.PublicURL != "" {
		r.ReceiptURL = s.cfg.PublicURL + "/payment/callback?paymentReference=" +
			url.QueryEscape(full.Reference)
	}

	s.mail.SendReceipt(r)
}

// naira renders kobo for a human. Integer arithmetic with a thousands
// separator; money never goes near a float on its way to a customer.
func naira(kobo int64) string {
	neg := kobo < 0
	if neg {
		kobo = -kobo
	}
	whole := strconv.FormatInt(kobo/100, 10)
	var out []byte
	for i, c := range []byte(whole) {
		if i > 0 && (len(whole)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s₦%s.%02d", sign, out, kobo%100)
}

// prettyMethod turns ACCOUNT_TRANSFER into "Account transfer".
func prettyMethod(m string) string {
	if m == "" {
		return ""
	}
	s := strings.ToLower(strings.ReplaceAll(m, "_", " "))
	return strings.ToUpper(s[:1]) + s[1:]
}
