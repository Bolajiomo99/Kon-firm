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

	"github.com/Bolajiomo99/Kon-firm/internal/events"
	"github.com/Bolajiomo99/Kon-firm/internal/monnify"
	"github.com/Bolajiomo99/Kon-firm/internal/store"
)

type refundRequest struct {
	// AmountKobo is optional; omit for a full refund.
	AmountKobo int64  `json:"amountKobo"`
	Reason     string `json:"reason"`
}

func newRefundReference() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("KFR-%d-%s", time.Now().Unix(), hex.EncodeToString(b)), nil
}

// handleRefund issues a refund against a paid order. Admin only.
//
// The sequence is: record the attempt, then call Monnify, then record the
// outcome. Calling Monnify first would risk money moving with nothing written
// down if the process died in between.
func (s *Server) handleRefund(w http.ResponseWriter, r *http.Request) {
	admin := userFrom(r.Context())
	orderRef := r.PathValue("reference")

	var req refundRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "a refund reason is required")
		return
	}

	order, err := s.store.OrderByReference(r.Context(), orderRef)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "order not found")
		return
	}
	if err != nil {
		s.log.Error("refund: load order", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load order")
		return
	}

	amount := req.AmountKobo
	if amount <= 0 {
		amount = order.TotalKobo // default: refund it all
	}
	if amount < monnify.MinRefundKobo {
		writeError(w, http.StatusBadRequest, "the smallest refund Monnify accepts is ₦100")
		return
	}

	refundRef, err := newRefundReference()
	if err != nil {
		s.log.Error("refund: mint reference", "err", err)
		writeError(w, http.StatusInternalServerError, "could not start refund")
		return
	}

	// Write the attempt down before any money can move.
	rf, err := s.store.BeginRefund(r.Context(), orderRef, refundRef, req.Reason, amount, admin.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "order not found")
		return
	case errors.Is(err, store.ErrNotRefundable):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		s.log.Error("refund: begin", "err", err, "order", orderRef)
		writeError(w, http.StatusInternalServerError, "could not start refund")
		return
	}

	resp, err := s.monnify.InitiateRefund(r.Context(), monnify.RefundRequest{
		TransactionReference: order.TransactionRef,
		RefundReference:      refundRef,
		RefundAmount:         float64(amount) / 100,
		RefundReason:         req.Reason,
		// Bank alerts allow 16 characters, so this cannot be the order
		// reference — it would be cut mid-string into nonsense.
		CustomerNote: "Kon-firm refund",
	})
	if err != nil {
		// Monnify refused. Mark the attempt failed rather than leaving a
		// pending row that blocks future attempts against this order.
		if _, serr := s.store.SettleRefund(r.Context(), refundRef, "failed", err.Error()); serr != nil {
			s.log.Error("refund: could not record failure", "err", serr, "refund", refundRef)
		}
		s.log.Error("refund: monnify rejected", "err", err, "order", orderRef)
		writeError(w, http.StatusBadGateway, "Monnify could not process this refund: "+err.Error())
		return
	}

	status := "pending"
	switch {
	case resp.Completed():
		status = "completed"
	case resp.Failed():
		status = "failed"
	}

	settled, err := s.store.SettleRefund(r.Context(), refundRef, status, resp.Comment)
	if err != nil {
		s.log.Error("refund: settle", "err", err, "refund", refundRef)
		writeError(w, http.StatusInternalServerError, "refund was sent but could not be recorded")
		return
	}

	s.log.Info("refund issued",
		"order", orderRef, "refund", refundRef, "amount_kobo", amount,
		"status", status, "by_admin", admin.ID)

	s.events.PublishOrder(events.TypeRefundIssued, orderRef, settled)

	writeJSON(w, http.StatusOK, map[string]any{
		"refund":        settled,
		"monnifyStatus": resp.RefundStatus,
		"refundType":    resp.RefundType,
		"comment":       resp.Comment,
	})
	_ = rf
}

// handleRefundWebhook applies SUCCESSFUL_REFUND / FAILED_REFUND events.
// Called from the main webhook handler once the signature has been verified.
func (s *Server) applyRefundEvent(w http.ResponseWriter, r *http.Request, event *monnify.WebhookEvent, rawBody []byte) {
	data, err := event.RefundData()
	if err != nil {
		s.log.Error("decode refund event", "err", err, "body", truncateBody(rawBody))
		writeError(w, http.StatusBadRequest, "malformed refund data")
		return
	}

	status := "pending"
	switch event.EventType {
	case monnify.EventSuccessfulRefund:
		status = "completed"
	case monnify.EventFailedRefund:
		status = "failed"
	}

	// The webhook_events ledger keeps this idempotent: a redelivered refund
	// notification is recorded once, so stock is restored once.
	if _, err := s.store.ApplyWebhook(r.Context(), store.PaymentResult{
		TransactionRef: data.RefundReference, // refunds are keyed by their own ref
		PaymentRef:     "",
		EventType:      event.EventType,
		RawPayload:     rawBody,
	}); err != nil && !errors.Is(err, store.ErrAlreadyProcessed) && !errors.Is(err, store.ErrNotFound) {
		s.log.Error("refund webhook: ledger", "err", err)
	} else if errors.Is(err, store.ErrAlreadyProcessed) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already processed"})
		return
	}

	settled, err := s.store.SettleRefund(r.Context(), data.RefundReference, status, data.Comment)
	if errors.Is(err, store.ErrNotFound) {
		// A refund issued from Monnify's dashboard rather than from Kon-firm.
		// Acknowledge it: retrying will never make us recognise it.
		s.log.Warn("refund webhook for an unknown refund", "ref", data.RefundReference)
		writeJSON(w, http.StatusOK, map[string]string{"status": "unknown refund"})
		return
	}
	if err != nil {
		s.log.Error("refund webhook: settle", "err", err, "ref", data.RefundReference)
		writeError(w, http.StatusInternalServerError, "could not record refund")
		return
	}

	s.log.Info("refund settled by webhook", "ref", settled.Reference, "status", settled.Status)
	s.events.PublishOrder(events.TypeRefundDone, settled.OrderRef, settled)
	writeJSON(w, http.StatusOK, map[string]string{"status": "processed"})
}
