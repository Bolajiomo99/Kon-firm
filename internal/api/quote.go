package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Bolajiomo99/Kon-firm/internal/store"
)

// quoteRequest asks the server what a basket costs.
type quoteRequest struct {
	Items       []checkoutLine `json:"items"`
	VoucherCode string         `json:"voucherCode"`
	State       string         `json:"state"`
}

type quoteResponse struct {
	store.Quote
	// VoucherError explains a rejected code without failing the whole quote:
	// a bad voucher should still show the shopper their total, not a blank
	// panel and an error.
	VoucherError string `json:"voucherError,omitempty"`
}

// handleQuote prices a basket without creating an order.
//
// The browser calls this whenever the basket, voucher, or delivery state
// changes. It exists so the shopper sees the real total — VAT, delivery and
// discount included — before they commit, rather than discovering it on
// Monnify's page. It is also the same arithmetic checkout uses, so the two
// cannot disagree.
func (s *Server) handleQuote(w http.ResponseWriter, r *http.Request) {
	var req quoteRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "basket is empty")
		return
	}

	lines := make([]store.CreateOrderLine, 0, len(req.Items))
	for _, it := range req.Items {
		lines = append(lines, store.CreateOrderLine{ProductID: it.ProductID, Quantity: it.Quantity})
	}

	// Priced from database rows, never from anything the browser sent.
	subtotal, err := s.store.PriceBasket(r.Context(), lines)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "one or more products are unavailable")
			return
		}
		s.log.Error("quote: price basket", "err", err)
		writeError(w, http.StatusInternalServerError, "could not price your basket")
		return
	}

	var discount int64
	var code, voucherErr string
	if c := strings.TrimSpace(req.VoucherCode); c != "" {
		v, err := s.store.VoucherByCode(r.Context(), c, subtotal)
		switch {
		case err == nil:
			discount = v.DiscountFor(subtotal)
			code = v.Code
		case errors.Is(err, store.ErrVoucherNotFound):
			voucherErr = "That code isn’t valid."
		case errors.Is(err, store.ErrVoucherExpired):
			voucherErr = "That code has expired."
		case errors.Is(err, store.ErrVoucherUsedUp):
			voucherErr = "That code has been fully redeemed."
		case errors.Is(err, store.ErrVoucherMinSpend):
			voucherErr = "Your basket is below this code’s minimum spend."
		default:
			s.log.Error("quote: voucher", "err", err)
			voucherErr = "Could not check that code."
		}
	}

	fee := store.DeliveryFee(subtotal-discount, req.State)
	q := store.BuildQuote(subtotal, discount, fee, code)

	writeJSON(w, http.StatusOK, quoteResponse{Quote: q, VoucherError: voucherErr})
}
