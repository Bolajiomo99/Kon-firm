// Package api holds Kon-firm's HTTP handlers.
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Bolajiomo99/Kon-firm/internal/config"
	"github.com/Bolajiomo99/Kon-firm/internal/monnify"
	"github.com/Bolajiomo99/Kon-firm/internal/store"
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

// Routes builds the HTTP handler. frontend is the embedded static site; the
// API and the pages are served by the same binary on the same origin, which
// is why there is no CORS configuration anywhere in this project.
func (s *Server) Routes(frontend fs.FS) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/products", s.handleListProducts)
	mux.HandleFunc("GET /api/products/barcode/{barcode}", s.handleProductByBarcode)
	mux.HandleFunc("POST /api/checkout", s.handleCheckout)
	mux.HandleFunc("GET /api/orders/{reference}", s.handleGetOrder)
	mux.HandleFunc("POST /api/webhooks/monnify", s.handleMonnifyWebhook)
	mux.HandleFunc("GET /api/admin/overview", s.handleAdminOverview)

	// Everything not under /api is the frontend.
	mux.Handle("/", newStaticHandler(frontend))

	return s.withSecurityHeaders(s.withLogging(mux))
}

// withSecurityHeaders applies a conservative baseline to every response.
//
// The CSP is strict because it can be: the frontend loads no third-party
// scripts, fonts, or images, so nothing legitimate needs a wider policy.
func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'", // small inline blocks in the page heads
		"img-src 'self' data:",
		"connect-src 'self'",
		"media-src 'self' blob:", // the POS camera stream
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self' https://sandbox.sdk.monnify.com https://sdk.monnify.com",
	}, "; ")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "DENY")
		// The camera is needed on /pos and nowhere else.
		h.Set("Permissions-Policy", "camera=(self), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
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
