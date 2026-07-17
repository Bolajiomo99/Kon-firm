package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

const (
	// SessionCookie is deliberately host-only and prefixed. __Host- tells the
	// browser to refuse the cookie unless it is Secure, path=/, and has no
	// Domain — which stops a subdomain from ever setting or overwriting it.
	SessionCookie = "__Host-konfirm_session"

	SessionDuration = 30 * 24 * time.Hour
	tokenBytes      = 32 // 256 bits
)

// NewSessionToken mints a session token.
//
// crypto/rand, not math/rand: a predictable token is a login bypass. 256 bits
// is far beyond guessable.
func NewSessionToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generating session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns what gets stored for a session token.
//
// The database holds only the hash, never the token itself. Anyone who reads
// the sessions table — a backup, a log, a SQL injection — gets values they
// cannot present as a cookie. This is the same reasoning as password hashing,
// but a plain SHA-256 suffices: the token is already 256 bits of entropy, so
// there is no dictionary to attack and nothing for a slow hash to buy.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SetSessionCookie writes the session cookie.
//
// secure=false is permitted only so that http://localhost works in
// development; __Host- requires Secure, so the name is downgraded there too.
func SetSessionCookie(w http.ResponseWriter, token string, secure bool) {
	name := SessionCookie
	if !secure {
		name = "konfirm_session"
	}
	http.SetCookie(w, &http.Cookie{
		Name:  name,
		Value: token,
		Path:  "/",
		// HttpOnly: JavaScript cannot read it, so an XSS bug cannot steal the
		// session outright.
		HttpOnly: true,
		Secure:   secure,
		// Lax: the cookie rides same-site navigations — including the return
		// from Monnify's checkout — but not cross-site POSTs, which is the CSRF
		// case that matters here.
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(SessionDuration),
		MaxAge:   int(SessionDuration.Seconds()),
	})
}

// ClearSessionCookie expires the cookie on logout.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	name := SessionCookie
	if !secure {
		name = "konfirm_session"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

// TokenFromRequest reads the session token from a request, accepting either
// cookie name so a session survives the dev/production distinction.
func TokenFromRequest(r *http.Request) string {
	for _, name := range []string{SessionCookie, "konfirm_session"} {
		if c, err := r.Cookie(name); err == nil && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// Roles a user may hold.
const (
	RoleCustomer = "customer"
	RoleAdmin    = "admin"
)
