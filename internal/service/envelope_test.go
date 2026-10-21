package service

import (
	"encoding/json"
	"strings"
	"testing"
)

// Panels in the wild return `status` as bool, 0/1, or "1"/"true". A strict bool
// field made every API call fail with a Go type error, so this locks the
// permissive decoding in place.
func TestAPIEnvelopeStatusForms(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		wantOK  bool
		present bool
	}{
		{"bool true", `{"status":true,"data":{"a":1}}`, true, true},
		{"bool false", `{"status":false,"data":null}`, false, true},
		{"number 1", `{"status":1,"data":{"a":1}}`, true, true},
		{"number 0", `{"status":0,"data":null}`, false, true},
		{"string 1", `{"status":"1","data":{"a":1}}`, true, true},
		{"string true", `{"status":"true","data":{"a":1}}`, true, true},
		{"string 0", `{"status":"0","data":null}`, false, true},
		{"string false", `{"status":"false","data":null}`, false, true},
		{"uppercase", `{"status":"TRUE","data":{}}`, true, true},
		{"absent", `{"data":{"a":1}}`, false, false},
		{"null", `{"status":null,"data":{"a":1}}`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var env apiEnvelope
			if err := json.Unmarshal([]byte(tc.payload), &env); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.payload, err)
			}
			if env.Status.present != tc.present {
				t.Errorf("present = %v, want %v", env.Status.present, tc.present)
			}
			if env.Status.present && env.Status.value != tc.wantOK {
				t.Errorf("value = %v, want %v", env.Status.value, tc.wantOK)
			}
		})
	}
}

// An unknown status literal must not abort the whole response: the caller should
// get a readable error, and a body without a status field must still parse.
func TestAPIEnvelopeStatusUnknownLiteral(t *testing.T) {
	var env apiEnvelope
	err := json.Unmarshal([]byte(`{"status":"weird","data":{}}`), &env)
	if err == nil {
		t.Fatal("expected an error for an unknown status literal")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Errorf("error should mention the status field, got %v", err)
	}
	if len(err.Error()) > 80 {
		t.Errorf("error should stay short, got %d chars", len(err.Error()))
	}
}

// A WAF/captcha interstitial is HTML. It must be reported as such, not as a Go
// decoder failure that reads like a client bug.
func TestLooksLikeHTML(t *testing.T) {
	html := []byte(`<!doctypehtml><meta charset="UTF-8"><title></title>`)
	if !looksLikeHTML(html) {
		t.Error("expected WAF page to be detected as HTML")
	}
	if !looksLikeHTML([]byte("  \n<html><body>x</body></html>")) {
		t.Error("expected leading whitespace to be tolerated")
	}
	if looksLikeHTML([]byte(`{"status":true,"data":{}}`)) {
		t.Error("JSON must not be classified as HTML")
	}
	if looksLikeHTML([]byte("   ")) {
		t.Error("blank body must not be classified as HTML")
	}
}
