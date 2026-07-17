// Package email sends order receipts.
//
// Plain SMTP over the standard library rather than a provider SDK. Any SMTP
// server works — Gmail with an app password, Resend, Postmark, a company relay
// — which matters because the alternative is a vendor's free tier deciding it
// will only deliver to a verified domain, on the morning of a demo.
//
// Nothing here may ever break a payment. Money has already moved by the time a
// receipt is sent; a mail server being slow or down is not a reason to fail a
// webhook Monnify is waiting on. Every send is fire-and-forget and every error
// is logged, not returned.
package email

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"mime"
	"net/smtp"
	"strings"
	"time"
)

// Config is the SMTP connection.
type Config struct {
	Host     string
	Port     string
	Username string
	Password string
	// From is the visible sender, e.g. "Kon-firm <orders@example.com>".
	From string
	// BaseURL builds the link back to the receipt page.
	BaseURL string
}

// Configured reports whether enough is set to send anything.
func (c Config) Configured() bool {
	return c.Host != "" && c.Port != "" && c.Username != "" && c.Password != "" && c.From != ""
}

type Sender struct {
	cfg Config
	log *slog.Logger
}

func New(cfg Config, log *slog.Logger) *Sender {
	if !cfg.Configured() {
		// Loud, because a shop that silently stops sending receipts looks
		// fine from every dashboard and broken to every customer.
		log.Warn("email is not configured — receipts will not be sent",
			"missing", cfg.missing())
	}
	return &Sender{cfg: cfg, log: log}
}

func (c Config) missing() string {
	var m []string
	for _, f := range []struct{ n, v string }{
		{"SMTP_HOST", c.Host}, {"SMTP_PORT", c.Port},
		{"SMTP_USERNAME", c.Username}, {"SMTP_PASSWORD", c.Password},
		{"SMTP_FROM", c.From},
	} {
		if f.v == "" {
			m = append(m, f.n)
		}
	}
	return strings.Join(m, ", ")
}

// ReceiptLine is one item on the receipt.
type ReceiptLine struct {
	Name     string
	Quantity int
	Amount   string
}

// Receipt is everything a customer needs to prove what they bought.
type Receipt struct {
	CustomerName  string
	Email         string
	Reference     string
	MonnifyRef    string
	PaidAt        time.Time
	PaymentMethod string
	Lines         []ReceiptLine
	Subtotal      string
	Discount      string
	VoucherCode   string
	Delivery      string
	FreeDelivery  bool
	Total         string
	VAT           string
	VATRate       string
	Address       string
	ReceiptURL    string
	Year          int
}

// SendReceipt emails a receipt. It never blocks the caller and never returns
// an error: the payment already succeeded, and nothing about a mail server is
// allowed to change that.
func (s *Sender) SendReceipt(r Receipt) {
	if !s.cfg.Configured() {
		s.log.Warn("receipt not sent — email is not configured", "ref", r.Reference, "to", r.Email)
		return
	}
	if !strings.Contains(r.Email, "@") {
		s.log.Warn("receipt not sent — no usable address", "ref", r.Reference)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- s.send(r) }()

		select {
		case err := <-done:
			if err != nil {
				s.log.Error("receipt failed to send", "err", err, "ref", r.Reference, "to", r.Email)
				return
			}
			s.log.Info("receipt sent", "ref", r.Reference, "to", r.Email)
		case <-ctx.Done():
			s.log.Error("receipt timed out", "ref", r.Reference, "to", r.Email)
		}
	}()
}

func (s *Sender) send(r Receipt) error {
	r.Year = time.Now().Year()

	var body bytes.Buffer
	if err := receiptTmpl.Execute(&body, r); err != nil {
		return fmt.Errorf("render receipt: %w", err)
	}

	subject := fmt.Sprintf("Your Kon-firm receipt — %s", r.Total)
	msg := buildMessage(s.cfg.From, r.Email, subject, body.String())

	addr := s.cfg.Host + ":" + s.cfg.Port
	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	return smtp.SendMail(addr, auth, senderAddress(s.cfg.From), []string{r.Email}, msg)
}

// senderAddress extracts the bare address from "Name <addr@host>", which is
// what the SMTP envelope needs — the display name belongs only in the header.
func senderAddress(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.LastIndex(from, ">"); j > i {
			return from[i+1 : j]
		}
	}
	return strings.TrimSpace(from)
}

func buildMessage(from, to, subject, html string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	// Encoded, so a naira sign or a Yoruba name in the subject is not mangled.
	fmt.Fprintf(&b, "Subject: %s\r\n", encodeHeader(subject))
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: text/html; charset=UTF-8\r\n")
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "\r\n")
	b.WriteString(html)
	return b.Bytes()
}

// encodeHeader RFC 2047-encodes a header. mime.QEncoding leaves plain ASCII
// untouched and encodes the rest, so a naira sign in the subject survives.
func encodeHeader(s string) string {
	return mime.QEncoding.Encode("UTF-8", s)
}
