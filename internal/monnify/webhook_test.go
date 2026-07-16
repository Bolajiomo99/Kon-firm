package monnify

import (
	"net/http/httptest"
	"testing"
)

// Cross-language fixture. Generated independently with Python:
//
//	python3 -c "import hmac,hashlib; print(hmac.new(b'test_secret_key',
//	  b'{\"eventType\":\"SUCCESSFUL_TRANSACTION\"}', hashlib.sha512).hexdigest())"
//
// If this vector ever fails, our HMAC construction has drifted from the
// standard one Monnify uses, and every webhook would be rejected in production.
const (
	fixtureSecret = "test_secret_key"
	fixtureBody   = `{"eventType":"SUCCESSFUL_TRANSACTION"}`
	fixtureSig    = "149c0f00188a70ed747fc4039682e056511a282f1901740176d51c1d898060a3" +
		"056a3a166e7bb1fe34a70a2f1b41edd02a96eefe2d282aade2dc2ff5ed377733"
)

// TestVerifySignature_MatchesIndependentImplementation pins our HMAC against a
// value produced by a different language's crypto library. This is the test
// that catches an algorithm drift before Monnify does.
func TestVerifySignature_MatchesIndependentImplementation(t *testing.T) {
	if got := hmacSHA512Hex(fixtureSecret, []byte(fixtureBody)); got != fixtureSig {
		t.Fatalf("HMAC drifted from the reference implementation:\n got  %s\n want %s", got, fixtureSig)
	}
	if !VerifySignature(fixtureSecret, []byte(fixtureBody), fixtureSig) {
		t.Error("the reference signature must verify")
	}
}

func TestVerifySignature(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		body   string
		sig    string
		want   bool
	}{
		{"empty signature rejected", fixtureSecret, fixtureBody, "", false},
		{"empty secret rejected", "", fixtureBody, "abcd", false},
		{"malformed hex rejected", fixtureSecret, fixtureBody, "not-hex-zzzz", false},
		{"wrong length rejected", fixtureSecret, fixtureBody, "abcd", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VerifySignature(tt.secret, []byte(tt.body), tt.sig); got != tt.want {
				t.Errorf("VerifySignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

// signFor mirrors what Monnify does on their side when signing a notification.
func signFor(t *testing.T, secret, body string) string {
	t.Helper()
	return hmacSHA512Hex(secret, []byte(body))
}

func TestVerifySignature_RoundTrip(t *testing.T) {
	sig := signFor(t, fixtureSecret, fixtureBody)

	if !VerifySignature(fixtureSecret, []byte(fixtureBody), sig) {
		t.Fatal("a correctly signed body must verify")
	}

	t.Run("tampered body fails", func(t *testing.T) {
		tampered := `{"eventType":"SUCCESSFUL_TRANSACTION","amountPaid":999999}`
		if VerifySignature(fixtureSecret, []byte(tampered), sig) {
			t.Error("a tampered body must not verify — this would let an attacker forge payments")
		}
	})

	t.Run("wrong secret fails", func(t *testing.T) {
		if VerifySignature("attacker_secret", []byte(fixtureBody), sig) {
			t.Error("a body signed with a different secret must not verify")
		}
	})

	t.Run("uppercase hex still verifies", func(t *testing.T) {
		upper := ""
		for _, c := range sig {
			if c >= 'a' && c <= 'f' {
				c = c - 'a' + 'A'
			}
			upper += string(c)
		}
		if !VerifySignature(fixtureSecret, []byte(fixtureBody), upper) {
			t.Error("hex casing must not affect verification")
		}
	})
}

func TestVerifyRequest(t *testing.T) {
	body := []byte(fixtureBody)
	sig := signFor(t, fixtureSecret, fixtureBody)

	t.Run("missing header", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/webhook", nil)
		if err := VerifyRequest(fixtureSecret, r, body); err == nil {
			t.Error("expected error when signature header is absent")
		}
	})

	t.Run("valid signature", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/webhook", nil)
		r.Header.Set(SignatureHeader, sig)
		if err := VerifyRequest(fixtureSecret, r, body); err != nil {
			t.Errorf("expected valid signature to pass, got %v", err)
		}
	})

	t.Run("bad signature", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/webhook", nil)
		r.Header.Set(SignatureHeader, signFor(t, "wrong", fixtureBody))
		if err := VerifyRequest(fixtureSecret, r, body); err == nil {
			t.Error("expected mismatched signature to be rejected")
		}
	})
}

func TestParseWebhook(t *testing.T) {
	t.Run("rejects malformed json", func(t *testing.T) {
		if _, err := ParseWebhook([]byte("{not json")); err == nil {
			t.Error("expected malformed body to error")
		}
	})

	t.Run("rejects missing eventType", func(t *testing.T) {
		if _, err := ParseWebhook([]byte(`{"eventData":{}}`)); err == nil {
			t.Error("expected missing eventType to error")
		}
	})

	t.Run("decodes transaction event", func(t *testing.T) {
		raw := []byte(`{
			"eventType":"SUCCESSFUL_TRANSACTION",
			"eventData":{
				"transactionReference":"MNFY|123",
				"paymentReference":"KF-ORDER-1",
				"amountPaid":5000.00,
				"totalPayable":5000.00,
				"paymentStatus":"PAID",
				"currency":"NGN",
				"customer":{"name":"Ada","email":"ada@example.com"}
			}
		}`)

		ev, err := ParseWebhook(raw)
		if err != nil {
			t.Fatalf("ParseWebhook: %v", err)
		}
		if ev.EventType != EventSuccessfulTransaction {
			t.Errorf("EventType = %q, want %q", ev.EventType, EventSuccessfulTransaction)
		}

		d, err := ev.TransactionData()
		if err != nil {
			t.Fatalf("TransactionData: %v", err)
		}
		if d.PaymentReference != "KF-ORDER-1" {
			t.Errorf("PaymentReference = %q, want KF-ORDER-1", d.PaymentReference)
		}
		if d.AmountPaid != 5000.00 {
			t.Errorf("AmountPaid = %v, want 5000", d.AmountPaid)
		}
		if d.Customer.Email != "ada@example.com" {
			t.Errorf("Customer.Email = %q", d.Customer.Email)
		}
	})
}

func TestNewClient_RejectsKeyEnvironmentMismatch(t *testing.T) {
	t.Run("test key against production", func(t *testing.T) {
		_, err := NewClient(Config{
			APIKey: "MK_TEST_ABC", SecretKey: "s", ContractCode: "c",
			BaseURL: ProductionBaseURL,
		})
		if err == nil {
			t.Error("MK_TEST_ key against production must be rejected")
		}
	})

	t.Run("prod key against sandbox", func(t *testing.T) {
		_, err := NewClient(Config{
			APIKey: "MK_PROD_ABC", SecretKey: "s", ContractCode: "c",
			BaseURL: SandboxBaseURL,
		})
		if err == nil {
			t.Error("MK_PROD_ key against sandbox must be rejected")
		}
	})

	t.Run("test key against sandbox is fine", func(t *testing.T) {
		_, err := NewClient(Config{
			APIKey: "MK_TEST_ABC", SecretKey: "s", ContractCode: "c",
			BaseURL: SandboxBaseURL,
		})
		if err != nil {
			t.Fatalf("expected valid config to succeed, got %v", err)
		}
	})

	t.Run("missing credentials rejected", func(t *testing.T) {
		for _, cfg := range []Config{
			{SecretKey: "s", ContractCode: "c"},
			{APIKey: "MK_TEST_A", ContractCode: "c"},
			{APIKey: "MK_TEST_A", SecretKey: "s"},
		} {
			if _, err := NewClient(cfg); err == nil {
				t.Errorf("expected incomplete config %+v to be rejected", cfg)
			}
		}
	})

	t.Run("defaults to sandbox", func(t *testing.T) {
		c, err := NewClient(Config{APIKey: "MK_TEST_A", SecretKey: "s", ContractCode: "c"})
		if err != nil {
			t.Fatal(err)
		}
		if c.cfg.BaseURL != SandboxBaseURL {
			t.Errorf("BaseURL = %q, want sandbox default", c.cfg.BaseURL)
		}
	})
}
