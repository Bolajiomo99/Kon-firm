package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Bolajiomo99/Kon-firm/internal/events"
	"github.com/Bolajiomo99/Kon-firm/internal/store"
)

type productPayload struct {
	SKU           string `json:"sku"`
	Barcode       string `json:"barcode"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	PriceKobo     int64  `json:"priceKobo"`
	CompareAtKobo *int64 `json:"compareAtKobo"`
	Stock         int    `json:"stock"`
	ImageURL      string `json:"imageUrl"`
	Category      string `json:"category"`
	IsNew         bool   `json:"isNew"`
	Active        *bool  `json:"active"`
}

// allowedImageHosts is where a product photo may come from.
//
// An admin pasting an arbitrary URL would break the page silently: the CSP
// only permits images from these origins, so anything else renders as nothing
// with a console error the admin will never look at. Rejecting it here, with a
// message, is kinder than a blank card.
var allowedImageHosts = []string{
	"https://images.unsplash.com/",
	"/img/", // bundled assets
}

func validImageURL(u string) bool {
	u = strings.TrimSpace(u)
	if u == "" {
		return true // no image is allowed; the card falls back to a label
	}
	for _, h := range allowedImageHosts {
		if strings.HasPrefix(u, h) {
			return true
		}
	}
	return false
}

func (p *productPayload) toInput() store.ProductInput {
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	return store.ProductInput{
		SKU: p.SKU, Barcode: p.Barcode, Name: p.Name, Description: p.Description,
		PriceKobo: p.PriceKobo, CompareAtKobo: p.CompareAtKobo, Stock: p.Stock,
		ImageURL: p.ImageURL, Category: p.Category, IsNew: p.IsNew, Active: active,
	}
}

func (s *Server) handleAdminListProducts(w http.ResponseWriter, r *http.Request) {
	products, err := s.store.ListAllProducts(r.Context())
	if err != nil {
		s.log.Error("admin list products", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load products")
		return
	}
	writeJSON(w, http.StatusOK, products)
}

func (s *Server) handleCreateProduct(w http.ResponseWriter, r *http.Request) {
	var p productPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validImageURL(p.ImageURL) {
		writeError(w, http.StatusBadRequest,
			"image must be an images.unsplash.com URL or a /img/ path — other hosts are blocked by the site's security policy and would show as a blank card")
		return
	}

	created, err := s.store.CreateProduct(r.Context(), p.toInput())
	switch {
	case errors.Is(err, store.ErrSKUTaken):
		writeError(w, http.StatusConflict, "that SKU already exists")
		return
	case err != nil:
		s.log.Error("create product", "err", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.log.Info("product created", "sku", created.SKU, "by_admin", userFrom(r.Context()).ID)
	s.events.Publish(events.TopicAdmin, events.Event{Type: events.TypeStockChanged, Data: created})
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateProduct(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid product id")
		return
	}

	var p productPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validImageURL(p.ImageURL) {
		writeError(w, http.StatusBadRequest,
			"image must be an images.unsplash.com URL or a /img/ path")
		return
	}

	updated, err := s.store.UpdateProduct(r.Context(), id, p.toInput())
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "product not found")
		return
	case errors.Is(err, store.ErrSKUTaken):
		writeError(w, http.StatusConflict, "that SKU belongs to another product")
		return
	case err != nil:
		s.log.Error("update product", "err", err, "id", id)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.log.Info("product updated", "sku", updated.SKU, "by_admin", userFrom(r.Context()).ID)
	s.events.Publish(events.TopicAdmin, events.Event{Type: events.TypeStockChanged, Data: updated})
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid product id")
		return
	}

	deactivated, err := s.store.DeleteProduct(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		s.log.Error("delete product", "err", err, "id", id)
		writeError(w, http.StatusInternalServerError, "could not remove product")
		return
	}

	s.log.Info("product removed", "id", id, "deactivated", deactivated,
		"by_admin", userFrom(r.Context()).ID)
	s.events.Publish(events.TopicAdmin, events.Event{Type: events.TypeStockChanged})

	if deactivated {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "deactivated",
			"note":   "This product has orders against it, so it was hidden from the shop rather than deleted — deleting it would erase lines from past receipts.",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
