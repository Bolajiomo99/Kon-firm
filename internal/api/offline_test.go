package api

import (
	"encoding/json"
	"testing"
)

// TestPayerVerificationResponseShape pins our response against the exact JSON
// Monnify's documentation specifies.
//
// This endpoint is called while a customer stands at an agent's counter with
// cash in hand. A field name we invented — or an integer where they expect a
// string — means the POS cannot read the answer and the sale simply fails,
// with no error anyone can see from here.
//
// Unlike the paidOn bug, the contract for THIS direction is fully documented,
// so the fixture below is their published shape rather than our assumption.
func TestPayerVerificationResponseShape(t *testing.T) {
	t.Run("success matches the documented body", func(t *testing.T) {
		got, _ := json.Marshal(payerVerificationResponse{
			ResponseCode:    offlineOK,
			ResponseMessage: "Payer exists",
			PayerName:       "Zainab Wahab",
		})
		var m map[string]any
		_ = json.Unmarshal(got, &m)

		if m["responseCode"] != "00" {
			t.Errorf("responseCode = %v, want the string \"00\"", m["responseCode"])
		}
		// A string, not a number. "00" and 0 are different values to their POS.
		if _, ok := m["responseCode"].(string); !ok {
			t.Error("responseCode must serialise as a string")
		}
		if m["payerName"] != "Zainab Wahab" {
			t.Errorf("payerName = %v", m["payerName"])
		}
	})

	t.Run("merchant invoice carries an amount", func(t *testing.T) {
		got, _ := json.Marshal(payerVerificationResponse{
			ResponseCode:    offlineOK,
			ResponseMessage: "Payer exists",
			PayerName:       "Zainab Wahab",
			Amount:          58500, // naira, as their field expects
		})
		var m map[string]any
		_ = json.Unmarshal(got, &m)
		if m["amount"] != float64(58500) {
			t.Errorf("amount = %v, want 58500", m["amount"])
		}
	})

	t.Run("failure omits payerName and amount", func(t *testing.T) {
		got, _ := json.Marshal(payerVerificationResponse{
			ResponseCode:    offlineNotFound,
			ResponseMessage: "Payer does not exist",
		})
		var m map[string]any
		_ = json.Unmarshal(got, &m)
		if _, present := m["payerName"]; present {
			t.Error("payerName must be omitted when the payer does not exist")
		}
		if _, present := m["amount"]; present {
			t.Error("amount must be omitted when there is nothing to collect")
		}
		if m["responseCode"] != "01" {
			t.Errorf("responseCode = %v, want \"01\"", m["responseCode"])
		}
	})
}

// TestPayerVerificationRequestDecodes uses Monnify's documented request body
// verbatim.
func TestPayerVerificationRequestDecodes(t *testing.T) {
	raw := []byte(`{"productCode":"P10101","paymentRecipientId":"LAHRAY101"}`)
	var req payerVerificationRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("Monnify's own example must decode: %v", err)
	}
	if req.ProductCode != "P10101" {
		t.Errorf("ProductCode = %q", req.ProductCode)
	}
	if req.PaymentRecipientId != "LAHRAY101" {
		t.Errorf("PaymentRecipientId = %q", req.PaymentRecipientId)
	}
}
