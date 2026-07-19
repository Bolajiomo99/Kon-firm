package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Bolajiomo99/Kon-firm/internal/store"
)

// Offline pay-ins: cash, at a Moniepoint agent.
//
// A customer with no card and no bank app walks into any Moniepoint location,
// gives the agent an order reference, and pays cash. Before the agent accepts
// a naira, Monnify calls the endpoint below to ask us whether that reference
// is real and what it should cost.
//
// This is the only Monnify integration where OUR server is the one being
// depended on in real time. If it is slow or wrong, an agent standing in front
// of a customer cannot take their money. So it does the minimum work possible:
// one indexed lookup, no side effects, and an answer in every branch.
//
// Response codes are Monnify's, not ours: "00" means proceed, anything else
// means stop. They are strings, not integers — "00" and "0" are different
// values to their POS.
const (
	offlineOK       = "00"
	offlineNotFound = "01"
	offlineError    = "02"
)

type payerVerificationRequest struct {
	// ProductCode is the offline product Monnify generated for us.
	ProductCode string `json:"productCode"`
	// PaymentRecipientId is what the customer gives the agent. For Kon-firm
	// that is the order reference, because it is the one thing a shopper
	// already has, can read aloud, and that identifies exactly one order.
	PaymentRecipientId string `json:"paymentRecipientId"`
}

type payerVerificationResponse struct {
	ResponseCode    string `json:"responseCode"`
	ResponseMessage string `json:"responseMessage"`
	PayerName       string `json:"payerName,omitempty"`
	// Amount is required for MERCHANT_INVOICE products: it is what the agent
	// will collect. In kobo? No — Monnify expects naira here, which is why it
	// is converted at this boundary and nowhere else.
	Amount float64 `json:"amount,omitempty"`
}

// handlePayerVerification answers Monnify's real-time question: "someone is
// standing at a counter with this reference — should I take their cash, and
// how much?"
//
// Deliberately NOT session-authenticated. Monnify calls this server-to-server
// with no cookie, and the reference itself is the credential: it is a
// timestamp plus 8 random bytes, unguessable, and knowing one only lets you
// pay for that order.
func (s *Server) handlePayerVerification(w http.ResponseWriter, r *http.Request) {
	var req payerVerificationRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
		// Even a malformed request gets a well-formed answer. An agent's POS
		// is waiting on this, and a bare 400 with no body would leave the
		// terminal guessing.
		s.log.Warn("payer verification: malformed request", "err", err)
		writeJSON(w, http.StatusOK, payerVerificationResponse{
			ResponseCode:    offlineError,
			ResponseMessage: "Could not read the request",
		})
		return
	}

	ref := strings.TrimSpace(req.PaymentRecipientId)
	s.log.Info("payer verification", "product", req.ProductCode, "recipient", ref)

	if ref == "" {
		writeJSON(w, http.StatusOK, payerVerificationResponse{
			ResponseCode:    offlineNotFound,
			ResponseMessage: "No order reference supplied",
		})
		return
	}

	order, err := s.store.OrderByReference(r.Context(), ref)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, payerVerificationResponse{
			ResponseCode:    offlineNotFound,
			ResponseMessage: "No order with that reference",
		})
		return
	}
	if err != nil {
		s.log.Error("payer verification: lookup failed", "err", err, "ref", ref)
		// Our fault, not the customer's. Tell the agent to stop rather than
		// take cash against an order we cannot read.
		writeJSON(w, http.StatusOK, payerVerificationResponse{
			ResponseCode:    offlineError,
			ResponseMessage: "Could not check that order, please try again",
		})
		return
	}

	// An order that is already settled must not be paid for twice. The agent
	// is the last place to catch this — after cash changes hands, undoing it
	// means a refund and an apology.
	switch order.Status {
	case "paid":
		writeJSON(w, http.StatusOK, payerVerificationResponse{
			ResponseCode:    offlineNotFound,
			ResponseMessage: "This order is already paid",
		})
		return
	case "refunded", "failed", "expired":
		writeJSON(w, http.StatusOK, payerVerificationResponse{
			ResponseCode:    offlineNotFound,
			ResponseMessage: "This order is closed",
		})
		return
	}

	writeJSON(w, http.StatusOK, payerVerificationResponse{
		ResponseCode:    offlineOK,
		ResponseMessage: "Payer exists",
		PayerName:       order.CustomerName,
		// Naira, not kobo. Monnify's field is a decimal amount and this is the
		// one place in the codebase money leaves integer arithmetic.
		Amount: float64(order.TotalKobo) / 100,
	})
}

// handlePaymentRequery answers "what happened to this transaction?" when
// Monnify's POS lost the outcome — a network drop mid-payment, typically.
//
// Optional in their spec, implemented because the alternative is an agent who
// took cash and cannot tell the customer whether it counted.
func (s *Server) handlePaymentRequery(w http.ResponseWriter, r *http.Request) {
	ref := strings.TrimSpace(r.URL.Query().Get("transactionReference"))
	if ref == "" {
		writeJSON(w, http.StatusOK, payerVerificationResponse{
			ResponseCode:    offlineError,
			ResponseMessage: "transactionReference is required",
		})
		return
	}

	order, err := s.store.OrderByReference(r.Context(), ref)
	if err != nil {
		writeJSON(w, http.StatusOK, payerVerificationResponse{
			ResponseCode:    offlineNotFound,
			ResponseMessage: "Unknown transaction",
		})
		return
	}

	code, msg := offlineNotFound, "Not paid"
	if order.Status == "paid" {
		code, msg = offlineOK, "Paid"
	}
	writeJSON(w, http.StatusOK, payerVerificationResponse{
		ResponseCode:    code,
		ResponseMessage: msg,
		PayerName:       order.CustomerName,
		Amount:          float64(order.TotalKobo) / 100,
	})
}
