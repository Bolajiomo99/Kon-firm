package auth

import (
	"strings"
	"testing"
)

func TestHashPassword_RoundTrip(t *testing.T) {
	const pw = "correct-horse-7"

	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash should be PHC argon2id format, got %.20s...", hash)
	}
	if strings.Contains(hash, pw) {
		t.Fatal("the hash contains the plaintext password")
	}

	ok, err := VerifyPassword(pw, hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("the correct password must verify")
	}

	ok, err = VerifyPassword("wrong-horse-7", hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Error("a wrong password must not verify")
	}
}

func TestHashPassword_SaltIsRandom(t *testing.T) {
	// Identical passwords must not produce identical hashes, or the table
	// reveals which users share a password.
	a, err := HashPassword("same-password-1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same-password-1")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two hashes of the same password are identical — the salt is not random")
	}

	// Both must still verify.
	for _, h := range []string{a, b} {
		ok, err := VerifyPassword("same-password-1", h)
		if err != nil || !ok {
			t.Errorf("hash %.24s... failed to verify: ok=%v err=%v", h, ok, err)
		}
	}
}

func TestVerifyPassword_RejectsMalformed(t *testing.T) {
	for _, h := range []string{
		"",
		"not-a-hash",
		"$argon2id$v=19$m=19456,t=2,p=1$short",
		"$bcrypt$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$badparams$c2FsdA$aGFzaA",
	} {
		ok, err := VerifyPassword("anything", h)
		if ok {
			t.Errorf("malformed hash %q must never verify", h)
		}
		if err == nil {
			t.Errorf("malformed hash %q should report an error", h)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		pw  string
		err error
	}{
		{"abc1", ErrPasswordTooShort},
		{"", ErrPasswordTooShort},
		{"password", ErrPasswordTooSimple}, // no digit
		{"12345678", ErrPasswordTooSimple}, // no letter
		{"password1", nil},
		{"Konfirm2026", nil},
		{strings.Repeat("a1", 65), ErrPasswordTooLong}, // 130 chars: argon2 DoS guard
	}
	for _, c := range cases {
		err := ValidatePassword(c.pw)
		if err != c.err {
			t.Errorf("ValidatePassword(%q) = %v, want %v", c.pw, err, c.err)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	// Every one of these is the same human being.
	same := []string{
		"08031234567",
		"8031234567",
		"+2348031234567",
		"2348031234567",
		"+234 803 123 4567",
		"0803-123-4567",
		"(0803) 123 4567",
	}
	for _, in := range same {
		got, err := NormalizePhone(in)
		if err != nil {
			t.Errorf("NormalizePhone(%q) errored: %v", in, err)
			continue
		}
		if got != "+2348031234567" {
			t.Errorf("NormalizePhone(%q) = %q, want +2348031234567 — one person must not get four accounts", in, got)
		}
	}

	bad := []string{
		"", "123", "abcdefghijk",
		"0603123456",   // too short
		"080312345678", // too long
		"+15551234567", // not Nigerian
		"06031234567",  // invalid operator prefix
	}
	for _, in := range bad {
		if got, err := NormalizePhone(in); err == nil {
			t.Errorf("NormalizePhone(%q) = %q, expected rejection", in, got)
		}
	}
}

func TestFormatPhoneForDisplay(t *testing.T) {
	if got := FormatPhoneForDisplay("+2348031234567"); got != "+234 803 123 4567" {
		t.Errorf("FormatPhoneForDisplay = %q", got)
	}
	// Anything unexpected passes through rather than being mangled.
	if got := FormatPhoneForDisplay("nonsense"); got != "nonsense" {
		t.Errorf("FormatPhoneForDisplay = %q", got)
	}
}

func TestSessionToken(t *testing.T) {
	a, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two session tokens collided — the generator is not random")
	}
	if len(a) < 40 {
		t.Errorf("token is only %d chars; want 256 bits of entropy", len(a))
	}

	// The stored value must not be the token itself.
	if HashToken(a) == a {
		t.Fatal("HashToken returned the token unchanged — the database would store a usable credential")
	}
	if HashToken(a) != HashToken(a) {
		t.Fatal("HashToken is not deterministic")
	}
	if HashToken(a) == HashToken(b) {
		t.Fatal("different tokens hash to the same value")
	}
}
