package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrPhoneTaken is returned when a phone number already has an account.
var ErrPhoneTaken = errors.New("store: that phone number is already registered")

type User struct {
	ID          int64      `json:"id"`
	Phone       string     `json:"phone"`
	Name        string     `json:"name"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`

	// PasswordHash is never serialised to JSON. The `-` tag is the only thing
	// standing between a careless writeJSON(user) and leaking the hash table.
	PasswordHash string `json:"-"`
}

// CreateUser registers an account. phone must already be normalised to E.164
// and passwordHash must already be an argon2id hash — this layer never sees a
// plaintext password.
func (s *Store) CreateUser(ctx context.Context, phone, name, email, passwordHash, role string) (*User, error) {
	if role == "" {
		role = "customer"
	}

	var u User
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (phone, name, email, password_hash, role)
		VALUES ($1,$2,$3,$4,$5::user_role)
		RETURNING id, phone, name, email, role::text, created_at`,
		phone, name, email, passwordHash, role).
		Scan(&u.ID, &u.Phone, &u.Name, &u.Email, &u.Role, &u.CreatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return nil, ErrPhoneTaken
	}
	if err != nil {
		return nil, fmt.Errorf("store: create user: %w", err)
	}
	return &u, nil
}

// UserByPhone loads an account for login, including the hash.
func (s *Store) UserByPhone(ctx context.Context, phone string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, phone, name, email, password_hash, role::text, created_at, last_login_at
		FROM users WHERE phone = $1`, phone).
		Scan(&u.ID, &u.Phone, &u.Name, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// TouchLogin records a successful sign-in.
func (s *Store) TouchLogin(ctx context.Context, userID int64) {
	// Best-effort: failing to record a timestamp must never fail a login.
	_, _ = s.pool.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, userID)
}

// CreateSession stores a session. tokenHash is the SHA-256 of the token; the
// token itself is never persisted.
func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time, userAgent string) error {
	if len(userAgent) > 500 {
		userAgent = userAgent[:500]
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at, user_agent)
		VALUES ($1,$2,$3,$4)`, tokenHash, userID, expiresAt, userAgent)
	if err != nil {
		return fmt.Errorf("store: create session: %w", err)
	}
	return nil
}

// UserBySessionToken resolves a session hash to its user.
//
// The expiry is enforced in SQL rather than in Go: an expired row must not be
// able to authenticate anyone even if a caller forgets to check.
func (s *Store) UserBySessionToken(ctx context.Context, tokenHash string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.phone, u.name, u.email, u.role::text, u.created_at, u.last_login_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash).
		Scan(&u.ID, &u.Phone, &u.Name, &u.Email, &u.Role, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// DeleteSession logs a session out.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// PurgeExpiredSessions clears out dead rows.
func (s *Store) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	ct, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// EnsureAdmin creates or updates the bootstrap admin account.
//
// An admin has to exist before anyone can log in to make one, so it comes from
// configuration. The password is re-hashed on every boot from the configured
// value, which means rotating it is a matter of changing the env var and
// redeploying — and that a leaked admin password cannot be quietly left in
// place by someone with database access.
func (s *Store) EnsureAdmin(ctx context.Context, phone, name, passwordHash string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (phone, name, password_hash, role)
		VALUES ($1, $2, $3, 'admin')
		ON CONFLICT (phone) DO UPDATE
		SET password_hash = EXCLUDED.password_hash,
		    role = 'admin',
		    name = EXCLUDED.name`,
		phone, name, passwordHash)
	if err != nil {
		return fmt.Errorf("store: ensure admin: %w", err)
	}
	return nil
}

// AttachOrderToUser links an order to the account that placed it.
func (s *Store) AttachOrderToUser(ctx context.Context, orderRef string, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE orders SET user_id = $2 WHERE reference = $1`, orderRef, userID)
	return err
}

// OrdersForUser lists a customer's own orders.
func (s *Store) OrdersForUser(ctx context.Context, userID int64) ([]Order, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, reference, COALESCE(transaction_ref,''), customer_name, customer_email,
		       total_kobo, status::text, channel::text, amount_paid_kobo,
		       payment_method, created_at, paid_at
		FROM orders WHERE user_id = $1 ORDER BY created_at DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Order{}
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.Reference, &o.TransactionRef, &o.CustomerName,
			&o.CustomerEmail, &o.TotalKobo, &o.Status, &o.Channel, &o.AmountPaidKobo,
			&o.PaymentMethod, &o.CreatedAt, &o.PaidAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
