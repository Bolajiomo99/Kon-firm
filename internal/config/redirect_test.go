package config

import "testing"

// TestCheckRedirectURL covers the failure that stranded a paying customer:
// Monnify took the money and the shopper had nowhere to land. It is silent
// server-side — nothing errors, nothing logs — so it has to be caught by
// configuration, not by a person.
func TestCheckRedirectURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		env     string
		wantBad bool
	}{
		{"the real thing", "https://konfirm.onrender.com/payment/callback", "production", false},
		{"localhost in development is fine", "http://localhost:8080/payment/callback", "development", false},

		{"empty", "", "production", true},
		{"the placeholder I told them to use", "https://placeholder.invalid", "production", true},
		{"example.com", "https://example.com/payment/callback", "production", true},
		{"localhost in production", "http://localhost:8080/payment/callback", "production", true},
		{"127.0.0.1 in production", "http://127.0.0.1:8080/payment/callback", "production", true},
		{"http in production", "http://konfirm.onrender.com/payment/callback", "production", true},
		{"not absolute", "/payment/callback", "production", true},
		{"right host, wrong path", "https://konfirm.onrender.com/", "production", true},
		{"homepage instead of the receipt", "https://konfirm.onrender.com/index.html", "production", true},
		{"trailing slash tolerated", "https://konfirm.onrender.com/payment/callback/", "production", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &Config{RedirectURL: c.url, Env: c.env}
			problem := cfg.CheckRedirectURL()
			if (problem != "") != c.wantBad {
				t.Errorf("CheckRedirectURL(%q, env=%s) = %q, wantBad=%v", c.url, c.env, problem, c.wantBad)
			}
		})
	}
}
