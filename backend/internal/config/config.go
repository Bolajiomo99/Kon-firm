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
func (c *Config) Redacted() string {
	return fmt.Sprintf("env=%s port=%s monnify_base=%s api_key=%s contract=%s db=%s",
		c.Env, c.Port, c.MonnifyBaseURL,
		mask(c.MonnifyAPIKey), mask(c.MonnifyContractCode), maskURL(c.DatabaseURL))
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
