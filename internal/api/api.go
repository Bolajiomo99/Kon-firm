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
	"github.com/Bolajiomo99/Kon-firm/internal/email"
	"github.com/Bolajiomo99/Kon-firm/internal/events"
	"github.com/Bolajiomo99/Kon-firm/internal/monnify"
	"github.com/Bolajiomo99/Kon-firm/internal/store"
)

type Server struct {
	cfg     *config.Config
	store   *store.Store
	monnify *monnify.Client
	log     *slog.Logger
	events  *events.Broker
	mail    *email.Sender
}

func NewServer(cfg *config.Config, st *store.Store, mc *monnify.Client, log *slog.Logger) *Server {
	return &Server{
		cfg: cfg, store: st, monnify: mc, log: log,
		events: events.NewBroker(),
		mail: email.New(email.Config{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort,
			Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
			From: cfg.SMTPFrom, BaseURL: cfg.PublicURL,
		}, log),
	}
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

	// Public.
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/products", s.handleListProducts)
	mux.HandleFunc("POST /api/quote", s.handleQuote)
	mux.HandleFunc("GET /api/geocode/reverse", s.handleReverseGeocode)
	mux.HandleFunc("POST /api/checkout", s.handleCheckout)
	mux.HandleFunc("GET /api/orders/{reference}", s.handleGetOrder)

	// Accounts.
	mux.HandleFunc("POST /api/auth/signup", s.handleSignup)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/me", s.handleMe)
	mux.HandleFunc("GET /api/stream", s.handleStream)
	mux.HandleFunc("GET /api/me/orders", s.requireUser(s.handleMyOrders))

	// Monnify calls this. It authenticates by signature, not by session.
	mux.HandleFunc("POST /api/webhooks/monnify", s.handleMonnifyWebhook)

	// Offline pay-ins. Monnify calls these server-to-server while a customer
	// stands at a Moniepoint counter, so they carry no session — the order
	// reference is the credential.
	mux.HandleFunc("POST /api/offline/verify-payer", s.handlePayerVerification)
	mux.HandleFunc("GET /api/offline/verify-payer", s.handleOfflineProbe)
	mux.HandleFunc("POST /api/offline/payment-request", s.handlePaymentRequest)
	mux.HandleFunc("GET /api/offline/requery", s.handlePaymentRequery)

	// Staff only. The POS is a shop-counter tool: barcode lookup exposes the
	// catalogue keyed by barcode, and taking payment is not a public action.
	mux.HandleFunc("GET /api/products/barcode/{barcode}", s.requireAdmin(s.handleProductByBarcode))
	mux.HandleFunc("GET /api/admin/overview", s.requireAdmin(s.handleAdminOverview))
	mux.HandleFunc("POST /api/admin/orders/{reference}/refund", s.requireAdmin(s.handleRefund))
	mux.HandleFunc("GET /api/admin/products", s.requireAdmin(s.handleAdminListProducts))
	mux.HandleFunc("POST /api/admin/products", s.requireAdmin(s.handleCreateProduct))
	mux.HandleFunc("PUT /api/admin/products/{id}", s.requireAdmin(s.handleUpdateProduct))
	mux.HandleFunc("DELETE /api/admin/products/{id}", s.requireAdmin(s.handleDeleteProduct))

	// Everything not under /api is the frontend.
	mux.Handle("/", newStaticHandler(frontend))

	// Order matters: withUser must run before any handler that reads the
	// session, and the security headers wrap the lot.
	return s.withSecurityHeaders(s.withLogging(s.withUser(mux)))
}

// withSecurityHeaders applies a conservative baseline to every response.
//
// The CSP stays as tight as the page allows. Product photography comes from
// Unsplash, so img-src names that one origin and nothing else does: scripts,
// styles, and fetches remain 'self', which means the image host can never run
// code in the page or be sent anything.
func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'", // small inline blocks in the page heads
		// Product photography is served from Unsplash's CDN. This is the only
		// third-party origin the page may touch, and only for images —
		// script-src and connect-src stay 'self', so the CDN can never run
		// code or receive data.
		"img-src 'self' data: https://images.unsplash.com",
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
		// Grant only what the site actually uses, to itself:
		//   camera      — the POS barcode scanner
		//   geolocation — "use my current location" at checkout
		//
		// `geolocation=()` is an empty allowlist, i.e. NOBODY may use it, not
		// "the default applies". Writing that here silently disabled the
		// feature site-wide and surfaced to the shopper as "permission
		// denied" — a browser reporting our own header back at us. The
		// difference between () and (self) is the whole feature.
		h.Set("Permissions-Policy", "camera=(self), geolocation=(self), microphone=(), payment=()")
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

// statusRecorder captures the response status for logging.
//
// Embedding http.ResponseWriter promotes its methods but NOT the optional
// interfaces a concrete writer also satisfies: a type assertion against the
// wrapper sees only what this struct declares. Without the Flush passthrough
// below, wrapping a handler in this middleware silently disables streaming —
// the SSE endpoint's w.(http.Flusher) check fails and every live update dies
// with a 500. Any wrapper around a ResponseWriter has to forward the
// interfaces it is standing in front of.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(c int) {
	r.status = c
	r.ResponseWriter.WriteHeader(c)
}

// Flush forwards to the underlying writer so Server-Sent Events still work
// through this middleware.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Pool().Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unreachable")
		return
	}

	// The redirect URL is reported here on purpose. It is public — a customer's
	// browser follows it — and a wrong one is otherwise undiagnosable from
	// outside: the payment succeeds and the shopper simply never comes back.
	body := map[string]any{
		"status":       "ok",
		"env":          s.cfg.Env,
		"monnifyBase":  s.cfg.MonnifyBaseURL,
		"redirectUrl":  s.cfg.RedirectURL,
		"receiptEmail": s.cfg.EmailConfigured(),
	}
	// Name what is missing rather than just saying no. "false" sends someone
	// hunting through a dashboard; a list of variable names does not. These
	// are names, never values — the password is never reported.
	if missing := s.cfg.MissingSMTP(); len(missing) > 0 {
		body["receiptEmailMissing"] = missing
	}
	if problem := s.cfg.CheckRedirectURL(); problem != "" {
		body["status"] = "degraded"
		body["redirectProblem"] = problem
	}
	writeJSON(w, http.StatusOK, body)
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
