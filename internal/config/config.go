// Package config loads Kon-firm settings from the environment.
//
// Values come from real environment variables first, falling back to a .env
// file for local development. Nothing here ever logs a secret.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	MonnifyAPIKey       string
	MonnifySecretKey    string
	MonnifyContractCode string
	MonnifyBaseURL      string
	RedirectURL         string
	DatabaseURL         string
	Port                string
	Env                 string

	// Bootstrap admin. An admin must exist before anyone can sign in to
	// create one, so the first account comes from configuration.
	AdminPhone    string
	AdminName     string
	AdminPassword string
}

// LoadDotEnv reads key=value pairs from path into the process environment.
// Existing environment variables win, so production config is never clobbered
// by a stray .env. A missing file is not an error.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		// Strip matching surrounding quotes, if present.
		if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, val); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// FindDotEnv walks up from the working directory looking for a .env file,
// so commands work from anywhere in the repo.
func FindDotEnv() string {
	dir, err := os.Getwd()
	if err != nil {
		return ".env"
	}
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, ".env")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ".env"
}

// Load builds a Config, loading .env first, and validates required fields.
func Load() (*Config, error) {
	if err := LoadDotEnv(FindDotEnv()); err != nil {
		return nil, fmt.Errorf("config: reading .env: %w", err)
	}

	c := &Config{
		MonnifyAPIKey:       os.Getenv("MONNIFY_API_KEY"),
		MonnifySecretKey:    os.Getenv("MONNIFY_SECRET_KEY"),
		MonnifyContractCode: os.Getenv("MONNIFY_CONTRACT_CODE"),
		MonnifyBaseURL:      os.Getenv("MONNIFY_BASE_URL"),
		RedirectURL:         os.Getenv("KONFIRM_REDIRECT_URL"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		Port:                os.Getenv("PORT"),
		Env:                 os.Getenv("ENV"),
		AdminPhone:          os.Getenv("KONFIRM_ADMIN_PHONE"),
		AdminName:           os.Getenv("KONFIRM_ADMIN_NAME"),
		AdminPassword:       os.Getenv("KONFIRM_ADMIN_PASSWORD"),
	}

	if c.AdminName == "" {
		c.AdminName = "Store Manager"
	}

	if c.Port == "" {
		c.Port = "8080"
	}
	if c.Env == "" {
		c.Env = "development"
	}
	if c.MonnifyBaseURL == "" {
		c.MonnifyBaseURL = "https://sandbox.monnify.com"
	}

	var missing []string
	for _, f := range []struct {
		name, val string
	}{
		{"MONNIFY_API_KEY", c.MonnifyAPIKey},
		{"MONNIFY_SECRET_KEY", c.MonnifySecretKey},
		{"MONNIFY_CONTRACT_CODE", c.MonnifyContractCode},
		{"DATABASE_URL", c.DatabaseURL},
	} {
		if f.val == "" || strings.Contains(f.val, "your_") || strings.Contains(f.val, "XXXXXXXXXX") {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("config: missing or placeholder values for: %s (copy .env.example to .env and fill it in)",
			strings.Join(missing, ", "))
	}

	return c, nil
}

// Redacted returns a loggable summary. It never includes secret material.
//
// redirect_url is included deliberately: it is not a secret, it is where
// Monnify sends the customer back to, and a wrong one strands them on the
// payment page with no way home — a failure that is invisible from the
// server and looks like the shop is broken.
func (c *Config) Redacted() string {
	return fmt.Sprintf("env=%s port=%s monnify_base=%s api_key=%s contract=%s redirect_url=%s db=%s",
		c.Env, c.Port, c.MonnifyBaseURL,
		mask(c.MonnifyAPIKey), mask(c.MonnifyContractCode), c.RedirectURL, maskURL(c.DatabaseURL))
}

// CheckRedirectURL reports why the configured redirect would strand a
// customer, or "" if it looks usable.
//
// This is checked at boot rather than discovered by a shopper. The failure is
// completely silent server-side: we hand Monnify a URL, Monnify takes the
// money, and the customer sits on a success page that goes nowhere. Nothing
// errors. Nothing logs. The only symptom is a person who paid and cannot get
// back to their receipt.
func (c *Config) CheckRedirectURL() string {
	u := strings.TrimSpace(c.RedirectURL)
	switch {
	case u == "":
		return "KONFIRM_REDIRECT_URL is empty — customers will have no way back from Monnify"
	case strings.Contains(u, "placeholder") || strings.Contains(u, ".invalid") || strings.Contains(u, "example.com"):
		return "KONFIRM_REDIRECT_URL is still a placeholder (" + u + ") — customers will be stranded on Monnify's page"
	case !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://"):
		return "KONFIRM_REDIRECT_URL must be an absolute http(s) URL, got: " + u
	}
	if c.Env == "production" {
		if strings.Contains(u, "localhost") || strings.Contains(u, "127.0.0.1") {
			return "KONFIRM_REDIRECT_URL points at localhost (" + u + ") in production — a customer's browser cannot reach your laptop"
		}
		if !strings.HasPrefix(u, "https://") {
			return "KONFIRM_REDIRECT_URL should be https in production, got: " + u
		}
	}
	if !strings.HasSuffix(strings.TrimRight(u, "/"), "/payment/callback") {
		return "KONFIRM_REDIRECT_URL does not end in /payment/callback (" + u + ") — that is the only page that renders a receipt"
	}
	return ""
}

func mask(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:4] + strings.Repeat("*", len(s)-4)
}

// maskURL keeps the host visible for debugging but drops any credentials.
func maskURL(s string) string {
	at := strings.LastIndex(s, "@")
	if at == -1 {
		return "<set>"
	}
	return "***@" + s[at+1:]
}
