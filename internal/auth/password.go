// Package auth handles credentials, sessions, and phone normalisation.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters.
//
// argon2id is the current recommendation for password hashing: it resists both
// GPU cracking (memory-hard) and side-channel attacks (the "id" hybrid). These
// figures follow OWASP's guidance — 19 MiB and 2 passes — which is tuned to be
// expensive for an attacker with a GPU farm while staying a few tens of
// milliseconds on an ordinary server.
//
// Never replace this with SHA-256 or bcrypt-with-low-cost. A fast hash is the
// whole vulnerability: it lets someone who steals the table try billions of
// guesses a second.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024 // KiB
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16
)

var (
	ErrInvalidHash        = errors.New("auth: password hash is malformed")
	ErrIncompatibleParams = errors.New("auth: password hash uses unsupported parameters")
	ErrPasswordTooShort   = errors.New("auth: password must be at least 8 characters")
	ErrPasswordTooLong    = errors.New("auth: password must be at most 128 characters")
	ErrPasswordTooSimple  = errors.New("auth: password must contain letters and numbers")
)

// ValidatePassword enforces a minimum standard before hashing.
//
// The upper bound is not arbitrary politeness: argon2 will happily chew on a
// megabyte-long password, which makes the login endpoint a denial-of-service
// vector.
func ValidatePassword(p string) error {
	if len(p) < 8 {
		return ErrPasswordTooShort
	}
	if len(p) > 128 {
		return ErrPasswordTooLong
	}
	var hasLetter, hasDigit bool
	for _, r := range p {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return ErrPasswordTooSimple
	}
	return nil
}

// HashPassword returns a PHC-format argon2id hash.
//
// The format embeds the parameters and salt, so the cost can be raised later
// without invalidating existing hashes: old ones keep verifying against the
// parameters they were made with.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generating salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches encodedHash.
//
// The comparison is constant-time. A byte-by-byte compare that returns early
// leaks, through timing, how much of the hash was correct.
func VerifyPassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrInvalidHash
	}
	if version != argon2.Version {
		return false, ErrIncompatibleParams
	}

	var memory uint32
	var timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}

	// Recompute using the stored parameters, not the current constants, so
	// hashes made under an older cost still verify.
	got := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(want)))

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
