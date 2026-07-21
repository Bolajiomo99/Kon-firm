package api

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// The fixtures in this file are pasted verbatim from Monnify's offline pay-ins
// guide. That is the whole point of them.
//
// An earlier version of this test asserted a field called "payerName", and its
// comment claimed to pin the response "against the exact JSON Monnify's
// documentation specifies". It did not. There is no payerName anywhere in their
// docs — the field is paymentRecipientDescription, the recipient id has to be
// echoed back, and "user does not exist" is code 02, not 01. The test passed
// because it asserted the shape I had invented, against the struct I had
// invented, and Monnify's integration team caught it in review instead.
//
// This is the second time in this project that self-written fixtures hid a
// contract mismatch; the first cost us two 400s on live webhooks. A test only
// tells you the truth if the fixture came from somewhere you do not control.
const (
	// Payer verification, MERCHANT_INVOICE product type — the variant Kon-firm
	// uses, because each basket has its own price.
	docPayerVerifiedInvoice = `{
    "responseCode": "00",
    "amount": 2000,
    "responseMessage": "User details retrieved successfully.",
    "paymentRecipientId": "21220002312312",
    "paymentRecipientDescription": "DAMILARE OGUNNAIKE SAMUEL"
}`
	docPayerMissing = `{
    "responseCode": "02",
    "responseMessage": "User does not exist."
}`
	docPayerRequest = `{
    "productCode": "P10101",
    "paymentRecipientId": "LAHRAY101"
}`
)

// keysOf returns the JSON object keys of v, sorted.
func keysOf(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func keysOfJSON(t *testing.T, raw string) []string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("Monnify's own documented JSON must parse: %v", err)
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestPayerVerificationMatchesDocumentedShape round-trips Monnify's published
// response through our struct and asserts nothing is lost, renamed, or added.
//
// Round-tripping is what catches an invented field: a name we made up survives
// marshalling perfectly and only shows up as a key THEY never asked for.
func TestPayerVerificationMatchesDocumentedShape(t *testing.T) {
	t.Run("success round-trips with no field lost or invented", func(t *testing.T) {
		var got payerVerificationResponse
		if err := json.Unmarshal([]byte(docPayerVerifiedInvoice), &got); err != nil {
			t.Fatalf("their documented response must decode into our struct: %v", err)
		}

		if want, have := keysOfJSON(t, docPayerVerifiedInvoice), keysOf(t, got); !reflect.DeepEqual(want, have) {
			t.Errorf("field names differ from the documentation:\n  want %v\n  got  %v", want, have)
		}

		if got.ResponseCode != "00" {
			t.Errorf("ResponseCode = %q, want \"00\"", got.ResponseCode)
		}
		if got.PaymentRecipientDescription != "DAMILARE OGUNNAIKE SAMUEL" {
			t.Errorf("PaymentRecipientDescription = %q", got.PaymentRecipientDescription)
		}
		if got.PaymentRecipientId != "21220002312312" {
			t.Errorf("PaymentRecipientId = %q", got.PaymentRecipientId)
		}
		if got.Amount != 2000 {
			t.Errorf("Amount = %v, want 2000", got.Amount)
		}
	})

	// "00" and 0 are different values to their POS.
	t.Run("responseCode serialises as a string", func(t *testing.T) {
		raw, _ := json.Marshal(payerVerificationResponse{ResponseCode: offlineOK})
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if _, ok := m["responseCode"].(string); !ok {
			t.Errorf("responseCode = %T, must be a string", m["responseCode"])
		}
	})

	t.Run("missing payer uses code 02, not 01", func(t *testing.T) {
		var doc payerVerificationResponse
		if err := json.Unmarshal([]byte(docPayerMissing), &doc); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if doc.ResponseCode != offlineNoPayer {
			t.Errorf("documented not-found code is %q, our constant is %q",
				doc.ResponseCode, offlineNoPayer)
		}
		if offlineNoPayer == offlineFailed {
			t.Error("the two refusal codes must stay distinct")
		}
	})

	t.Run("refusal omits money and the payer description", func(t *testing.T) {
		raw, _ := json.Marshal(payerVerificationResponse{
			ResponseCode:    offlineNoPayer,
			ResponseMessage: "User does not exist.",
		})
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		for _, k := range []string{"amount", "paymentRecipientDescription"} {
			if _, present := m[k]; present {
				t.Errorf("%s must be omitted when we are refusing the payment", k)
			}
		}
	})
}

// TestPayerVerificationRequestDecodes uses Monnify's documented request body
// verbatim.
func TestPayerVerificationRequestDecodes(t *testing.T) {
	var req payerVerificationRequest
	if err := json.Unmarshal([]byte(docPayerRequest), &req); err != nil {
		t.Fatalf("Monnify's own example must decode: %v", err)
	}
	if req.ProductCode != "P10101" {
		t.Errorf("ProductCode = %q", req.ProductCode)
	}
	if req.PaymentRecipientId != "LAHRAY101" {
		t.Errorf("PaymentRecipientId = %q", req.PaymentRecipientId)
	}
}

// TestRequeryMatchesDocumentedShape pins the requery response, which is keyed on
// Monnify's transaction reference and looks nothing like payer verification.
func TestRequeryMatchesDocumentedShape(t *testing.T) {
	// Their documented success body. Note it carries no amount: by requery time
	// the money question is settled and only the outcome is in doubt.
	const docRequerySuccess = `{
    "responseCode": "00",
    "productCode": "121221212",
    "paymentRecipientId": "LAHRAY101",
    "transactionReference": "MNFY|66|20210825115615|000002"
}`
	var got requeryResponse
	if err := json.Unmarshal([]byte(docRequerySuccess), &got); err != nil {
		t.Fatalf("their documented requery response must decode: %v", err)
	}
	if want, have := keysOfJSON(t, docRequerySuccess), keysOf(t, got); !reflect.DeepEqual(want, have) {
		t.Errorf("requery field names differ:\n  want %v\n  got  %v", want, have)
	}
	// The pipes must survive intact: net/http has already percent-decoded the
	// query string, so decoding it a second time would corrupt this.
	if got.TransactionReference != "MNFY|66|20210825115615|000002" {
		t.Errorf("TransactionReference = %q", got.TransactionReference)
	}

	t.Run("failure uses code 01", func(t *testing.T) {
		var f requeryResponse
		if err := json.Unmarshal([]byte(
			`{"responseCode":"01","responseMessage":"Reason for failed response"}`), &f); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if f.ResponseCode != offlineFailed {
			t.Errorf("documented requery failure code is %q, our constant is %q",
				f.ResponseCode, offlineFailed)
		}
	})
}
