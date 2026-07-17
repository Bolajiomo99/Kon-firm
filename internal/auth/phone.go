package auth

import (
	"errors"
	"strings"
)

var ErrInvalidPhone = errors.New("auth: not a valid Nigerian phone number")

// NormalizePhone converts Nigerian phone numbers to E.164 (+234...).
//
// The same person will type their number four different ways — 08031234567,
// 8031234567, +2348031234567, 234 803 123 4567 — and each must resolve to one
// identity. Storing them as typed would let one person register four accounts
// and then fail to log into any of them, because the lookup would not match
// what they typed the next time.
//
// Nigerian mobile numbers are 11 digits nationally: a leading 0, then a
// 3-digit operator prefix, then 7 subscriber digits.
func NormalizePhone(raw string) (string, error) {
	// Strip everything a human might add: spaces, dashes, brackets, dots.
	var digits strings.Builder
	hasPlus := strings.HasPrefix(strings.TrimSpace(raw), "+")
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()

	switch {
	case strings.HasPrefix(d, "234") && len(d) == 13:
		// 2348031234567 — already international.
		d = d[3:]
	case strings.HasPrefix(d, "0") && len(d) == 11:
		// 08031234567 — national format.
		d = d[1:]
	case len(d) == 10 && !hasPlus:
		// 8031234567 — leading zero omitted.
	default:
		return "", ErrInvalidPhone
	}

	if len(d) != 10 {
		return "", ErrInvalidPhone
	}

	// Nigerian mobile prefixes start 7, 8 or 9 after the country code.
	switch d[0] {
	case '7', '8', '9':
	default:
		return "", ErrInvalidPhone
	}

	return "+234" + d, nil
}

// FormatPhoneForDisplay renders a stored E.164 number the way a Nigerian
// reader expects: +234 803 123 4567.
func FormatPhoneForDisplay(e164 string) string {
	if !strings.HasPrefix(e164, "+234") || len(e164) != 14 {
		return e164
	}
	d := e164[4:]
	return "+234 " + d[0:3] + " " + d[3:6] + " " + d[6:]
}
