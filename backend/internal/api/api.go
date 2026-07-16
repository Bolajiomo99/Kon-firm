// Package api holds Kon-firm's HTTP handlers.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/konfirm/konfirm/backend/internal/config"
	"github.com/konfirm/konfirm/backend/internal/monnify"
	"github.com/konfirm/konfirm/backend/internal/store"
)

type Server struct {
	cfg     *config.Config
	store   *store.Store
	monnify *monnify.Client
	log     *slog.Logger
}

func NewServer(cfg *config.Config, st *store.Store, mc *monnify.Client, log *slog.Logger) *Server {
	return &Server{cfg: cfg, store: st, monnify: mc, log: log}
}

// writeJSON sends a JSON response. Errors sent to clients are deliberately
// terse: internal detail belongs in logs, not in an HTTP body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

// Routes builds the HTTP handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/products", s.handleListProducts)
	mux.HandleFunc("GET /api/products/barcode/{barcode}", s.handleProductByBarcode)
	mux.HandleFunc("POST /api/checkout", s.handleCheckout)
	mux.HandleFunc("GET /api/orders/{reference}", s.handleGetOrder)
	mux.HandleFunc("POST /api/webhooks/monnify", s.handleMonnifyWebhook)

	return s.withLogging(mux)
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("http",
			"method", r.Method, "path", r.URL.Path,
			"status", rec.status, "dur", time.Since(start).String())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(c int) {
	r.status = c
	r.ResponseWriter.WriteHeader(c)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Pool().Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unreachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListProducts(w http.ResponseWriter, r *http.Request) {
	products, err := s.store.ListProducts(r.Context())
	if err != nil {
		s.log.Error("list products", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load products")
		return
	}
	if products == nil {
		products = []store.Product{}
	}
	writeJSON(w, http.StatusOK, products)
}
