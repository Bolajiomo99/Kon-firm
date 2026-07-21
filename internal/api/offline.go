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
//
// The two non-zero codes are not interchangeable, and the docs use them on
// different endpoints: payer verification answers "02" when the payer does not
// exist, while the requery endpoint answers "01" for a general failure. An
// earlier version of this file used "01" for "not found" on both, which is the
// wrong code on the one endpoint that is mandatory.
const (
	offlineOK      = "00" // proceed, take the cash
	offlineFailed  = "01" // requery/payment-request: this attempt failed
	offlineNoPayer = "02" // payer verification: do not take the cash
)

type payerVerificationRequest struct {
	// ProductCode is the offline product Monnify generated for us.
	ProductCode string `json:"productCode"`
	// PaymentRecipientId is what the customer gives the agent. For Kon-firm
	// that is the order reference, because it is the one thing a shopper
	// already has, can read aloud, and that identifies exactly one order.
	PaymentRecipientId string `json:"paymentRecipientId"`
}

// payerVerificationResponse is Monnify's shape, copied field-for-field from
// their offline pay-ins guide. The names are theirs and cannot be improved on:
// this struct is parsed by their POS, not by us.
//
// It is the MERCHANT_INVOICE variant, which carries `amount`. That product type
// lets the merchant set the price per payer at verification time — which is
// exactly what a shopping basket is. A Fixed product would force every order to
// cost the same, and a Variable product would let the customer type in any
// number they liked.
type payerVerificationResponse struct {
	ResponseCode    string `json:"responseCode"`
	ResponseMessage string `json:"responseMessage"`
	// Amount is what the agent will collect. In kobo? No — Monnify expects
	// naira here, which is why it is converted at this boundary and nowhere
	// else in the codebase.
	Amount float64 `json:"amount,omitempty"`
	// PaymentRecipientId is echoed back so their POS can match the answer to
	// the question it asked.
	PaymentRecipientId string `json:"paymentRecipientId,omitempty"`
	// PaymentRecipientDescription is what the agent reads off the terminal to
	// confirm they have the right person before taking money. NOT "payerName",
	// which is a field this file used to invent.
	PaymentRecipientDescription string `json:"paymentRecipientDescription,omitempty"`
}

// requeryResponse answers "what happened to this transaction?" and has a
// different shape from payer verification — it is keyed on Monnify's
// transaction reference, not on ours.
type requeryResponse struct {
	ResponseCode       string `json:"responseCode"`
	ResponseMessage    string `json:"responseMessage,omitempty"`
	ProductCode        string `json:"productCode,omitempty"`
	PaymentRecipientId string `json:"paymentRecipientId,omitempty"`
	// TransactionReference is Monnify's reference, arriving URL-encoded and
	// decoded before it reaches us.
	TransactionReference string `json:"transactionReference,omitempty"`
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
			ResponseCode:    offlineNoPayer,
			ResponseMessage: "Could not read the request",
		})
		return
	}

	ref := strings.TrimSpace(req.PaymentRecipientId)
	s.log.Info("payer verification", "product", req.ProductCode, "recipient", ref)

	// refuse answers with the code that tells the agent to stop. Every branch
	// echoes the reference back so their POS can match answer to question.
	refuse := func(msg string) {
		writeJSON(w, http.StatusOK, payerVerificationResponse{
			ResponseCode:       offlineNoPayer,
			ResponseMessage:    msg,
			PaymentRecipientId: ref,
		})
	}

	if ref == "" {
		refuse("No order reference supplied")
		return
	}

	order, err := s.store.OrderByReference(r.Context(), ref)
	if errors.Is(err, store.ErrNotFound) {
		refuse("User does not exist.")
		return
	}
	if err != nil {
		s.log.Error("payer verification: lookup failed", "err", err, "ref", ref)
		// Our fault, not the customer's. Tell the agent to stop rather than
		// take cash against an order we cannot read.
		refuse("Could not check that order, please try again")
		return
	}

	// An order that is already settled must not be paid for twice. The agent
	// is the last place to catch this — after cash changes hands, undoing it
	// means a refund and an apology.
	switch order.Status {
	case "paid":
		refuse("This order is already paid")
		return
	case "refunded", "failed", "expired":
		refuse("This order is closed")
		return
	}

	writeJSON(w, http.StatusOK, payerVerificationResponse{
		ResponseCode:    offlineOK,
		ResponseMessage: "User details retrieved successfully.",
		// Naira, not kobo. Monnify's field is a decimal amount and this is the
		// one place in the codebase money leaves integer arithmetic.
		Amount:                      float64(order.TotalKobo) / 100,
		PaymentRecipientId:          order.Reference,
		PaymentRecipientDescription: order.CustomerName,
	})
}

// handleOfflineProbe answers a browser GET on the payer verification URL.
//
// Monnify calls that endpoint with POST. A GET would otherwise fall through to
// the storefront's 404 page — and the first thing anyone does when handed an
// integration URL is paste it into a browser. Someone checking whether we are
// ready would see "That page isn't here" and conclude we had not built it.
//
// So this exists purely so that a human pointing a browser at us gets told the
// truth: the endpoint is live, and it wants a POST.
func (s *Server) handleOfflineProbe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ready",
		"endpoint": "payer verification",
		"method":   "POST",
		"expects":  payerVerificationRequest{ProductCode: "<offline product code>", PaymentRecipientId: "<order reference>"},
		"returns": payerVerificationResponse{
			ResponseCode:                "00",
			ResponseMessage:             "User details retrieved successfully.",
			Amount:                      2000,
			PaymentRecipientId:          "<order reference>",
			PaymentRecipientDescription: "<customer name>",
		},
		"productType": "MERCHANT_INVOICE",
		"note":        "This URL is live and answers POST. A browser sends GET, hence this message.",
	})
}

// handlePaymentRequery answers "what happened to this transaction?" when
// Monnify's POS lost the outcome — a network drop mid-payment, typically.
//
// Optional in their spec, implemented because the alternative is an agent who
// took cash and cannot tell the customer whether it counted.
func (s *Server) handlePaymentRequery(w http.ResponseWriter, r *http.Request) {
	// Monnify sends its own reference URL-encoded — "MNFY%7C44%7C..." — and the
	// docs say to decode it. net/http has already done that by the time Query()
	// returns, so the pipes are back. Decoding again here would be wrong.
	ref := strings.TrimSpace(r.URL.Query().Get("transactionReference"))
	if ref == "" {
		writeJSON(w, http.StatusOK, requeryResponse{
			ResponseCode:    offlineFailed,
			ResponseMessage: "transactionReference is required",
		})
		return
	}

	// The reference here is Monnify's, not ours, so look up by what we recorded
	// against the order when the payment settled.
	order, err := s.store.OrderByTransactionRef(r.Context(), ref)
	if err != nil {
		writeJSON(w, http.StatusOK, requeryResponse{
			ResponseCode:         offlineFailed,
			ResponseMessage:      "Unknown transaction",
			TransactionReference: ref,
		})
		return
	}

	if order.Status != "paid" {
		writeJSON(w, http.StatusOK, requeryResponse{
			ResponseCode:         offlineFailed,
			ResponseMessage:      "Payment not completed",
			PaymentRecipientId:   order.Reference,
			TransactionReference: ref,
		})
		return
	}

	writeJSON(w, http.StatusOK, requeryResponse{
		ResponseCode:         offlineOK,
		ResponseMessage:      "Payment successful",
		PaymentRecipientId:   order.Reference,
		TransactionReference: ref,
	})
}
