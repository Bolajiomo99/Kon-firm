package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Bolajiomo99/Kon-firm/internal/auth"
	"github.com/Bolajiomo99/Kon-firm/internal/store"
)

type ctxKey string

const userCtxKey ctxKey = "konfirm.user"

// userFrom returns the authenticated user, if any.
func userFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(userCtxKey).(*store.User)
	return u
}

// secureCookies reports whether cookies should carry the Secure flag.
// Localhost is served over plain HTTP, so demanding Secure there would make
// login silently fail in development.
func (s *Server) secureCookies() bool { return s.cfg.Env == "production" }

// withUser attaches the session's user to the request context when present.
// It never rejects: it is context, not a gate. requireAuth does the gating.
func (s *Server) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := auth.TokenFromRequest(r)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		user, err := s.store.UserBySessionToken(r.Context(), auth.HashToken(token))
		if err != nil {
			// Unknown or expired session: clear the stale cookie so the browser
			// stops sending it on every request.
			if errors.Is(err, store.ErrNotFound) {
				auth.ClearSessionCookie(w, s.secureCookies())
			}
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, user)))
	})
}

// requireAdmin gates a handler to admins.
//
// It answers 404 rather than 403 to a non-admin. A 403 confirms the endpoint
// exists and that the caller merely lacks a role, which is a map of the admin
// surface for anyone probing. 404 says nothing.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r.Context())
		if u == nil || u.Role != auth.RoleAdmin {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		next(w, r)
	}
}

// requireUser gates a handler to any signed-in account.
func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r.Context()) == nil {
			writeError(w, http.StatusUnauthorized, "please sign in")
			return
		}
		next(w, r)
	}
}

type signupRequest struct {
	Phone    string `json:"phone"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID          int64  `json:"id"`
	Phone       string `json:"phone"`
	PhonePretty string `json:"phonePretty"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
}

func toUserResponse(u *store.User) userResponse {
	return userResponse{
		ID: u.ID, Phone: u.Phone, PhonePretty: auth.FormatPhoneForDisplay(u.Phone),
		Name: u.Name, Email: u.Email, Role: u.Role,
	}
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "please enter your name")
		return
	}

	phone, err := auth.NormalizePhone(req.Phone)
	if err != nil {
		writeError(w, http.StatusBadRequest, "please enter a valid Nigerian WhatsApp number, e.g. 0803 123 4567")
		return
	}

	if err := auth.ValidatePassword(req.Password); err != nil {
		switch {
		case errors.Is(err, auth.ErrPasswordTooShort):
			writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		case errors.Is(err, auth.ErrPasswordTooLong):
			writeError(w, http.StatusBadRequest, "password is too long")
		default:
			writeError(w, http.StatusBadRequest, "password must contain both letters and numbers")
		}
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.log.Error("hash password", "err", err)
		writeError(w, http.StatusInternalServerError, "could not create your account")
		return
	}

	// Role is never taken from the request. A self-service signup that could
	// name its own role would be an admin account for the asking.
	user, err := s.store.CreateUser(r.Context(), phone, req.Name, req.Email, hash, auth.RoleCustomer)
	if errors.Is(err, store.ErrPhoneTaken) {
		writeError(w, http.StatusConflict, "that number already has an account — please sign in instead")
		return
	}
	if err != nil {
		s.log.Error("create user", "err", err)
		writeError(w, http.StatusInternalServerError, "could not create your account")
		return
	}

	if err := s.startSession(w, r, user); err != nil {
		s.log.Error("start session", "err", err)
		writeError(w, http.StatusInternalServerError, "account created, but sign-in failed — please log in")
		return
	}

	s.log.Info("account created", "user_id", user.ID)
	writeJSON(w, http.StatusCreated, toUserResponse(user))
}

type loginRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	phone, err := auth.NormalizePhone(req.Phone)
	if err != nil {
		// Same message as a wrong password, deliberately: see below.
		writeError(w, http.StatusUnauthorized, "incorrect phone number or password")
		return
	}

	user, err := s.store.UserByPhone(r.Context(), phone)
	if err != nil {
		// Do not reveal whether the account exists. A distinct "no such user"
		// lets anyone enumerate which numbers are registered — which, for a
		// store, is a customer list.
		//
		// Hash a dummy anyway so a missing account does not return measurably
		// faster than a wrong password.
		_, _ = auth.VerifyPassword(req.Password, dummyHash)
		writeError(w, http.StatusUnauthorized, "incorrect phone number or password")
		return
	}

	ok, err := auth.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil {
		s.log.Error("verify password", "err", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "could not sign you in")
		return
	}
	if !ok {
		s.log.Warn("failed login", "user_id", user.ID, "remote", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "incorrect phone number or password")
		return
	}

	if err := s.startSession(w, r, user); err != nil {
		s.log.Error("start session", "err", err)
		writeError(w, http.StatusInternalServerError, "could not sign you in")
		return
	}

	s.store.TouchLogin(r.Context(), user.ID)
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

// dummyHash is a real argon2id hash of a random value, used to spend the same
// CPU time on a login for an account that does not exist. Without it, a
// missing account returns in microseconds and a real one in ~50ms, and that
// difference alone enumerates the customer list.
var dummyHash = func() string {
	h, err := auth.HashPassword("not-a-real-password-9")
	if err != nil {
		// Unreachable outside a broken crypto/rand.
		panic("api: cannot build dummy hash: " + err.Error())
	}
	return h
}()

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user *store.User) error {
	token, err := auth.NewSessionToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(auth.SessionDuration)
	if err := s.store.CreateSession(r.Context(), auth.HashToken(token), user.ID, expires, r.UserAgent()); err != nil {
		return err
	}
	auth.SetSessionCookie(w, token, s.secureCookies())
	return nil
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if token := auth.TokenFromRequest(r); token != "" {
		if err := s.store.DeleteSession(r.Context(), auth.HashToken(token)); err != nil {
			s.log.Error("delete session", "err", err)
		}
	}
	auth.ClearSessionCookie(w, s.secureCookies())
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed out"})
}

// handleMe reports the current session, so the frontend can render the right
// navigation without guessing.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	if u == nil {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user":          toUserResponse(u),
	})
}

// handleMyOrders lists the signed-in customer's own orders.
func (s *Server) handleMyOrders(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	orders, err := s.store.OrdersForUser(r.Context(), u.ID)
	if err != nil {
		s.log.Error("orders for user", "err", err, "user_id", u.ID)
		writeError(w, http.StatusInternalServerError, "could not load your orders")
		return
	}
	writeJSON(w, http.StatusOK, orders)
}
