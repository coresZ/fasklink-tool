package httpapi

import (
	"encoding/base64"
	"strings"
	"testing"
)

// The panel serves its own subscription addresses over plain http on the LAN,
// and the decrypt tool is the documented way to inspect them. Rejecting http://
// made the tool unusable for exactly the links the panel hands out.
func TestExtractTargetAcceptsHTTPAndHTTPS(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"https", "https://example.com/sub?token=abc", "https://example.com/sub?token=abc"},
		{"http", "http://192.168.1.10:8787/sub?key=abc", "http://192.168.1.10:8787/sub?key=abc"},
		{"http with padding", "  http://h/sub  \n", "http://h/sub"},
		{"https uppercase scheme not normalised", "HTTPS://example.com/sub", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractTarget(tc.in)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractTarget(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractTargetUnwrapsB64AndURL(t *testing.T) {
	inner := "http://192.168.1.10:8787/sub?key=abc"
	b64 := base64.URLEncoding.EncodeToString([]byte(inner))

	got, err := extractTarget("/sub?b64=" + b64)
	if err != nil {
		t.Fatalf("b64 wrapper: %v", err)
	}
	if got != inner {
		t.Fatalf("b64 wrapper: got %q, want %q", got, inner)
	}

	got, err = extractTarget("/sub?url=" + inner)
	if err != nil {
		t.Fatalf("url wrapper: %v", err)
	}
	if got != inner {
		t.Fatalf("url wrapper: got %q, want %q", got, inner)
	}

	// A bare base64 payload that decodes to an http URL must also be accepted.
	got, err = extractTarget(b64)
	if err != nil {
		t.Fatalf("bare b64: %v", err)
	}
	if got != inner {
		t.Fatalf("bare b64: got %q, want %q", got, inner)
	}
}

func TestExtractTargetRejectsGarbage(t *testing.T) {
	// Empty input gets its own message; other garbage must point at the expected form.
	blank := []string{"", "   "}
	for _, in := range blank {
		if got, err := extractTarget(in); err == nil {
			t.Errorf("extractTarget(%q) should fail, got %q", in, got)
		} else if !strings.Contains(err.Error(), "订阅链接") {
			t.Errorf("error for %q should mention 订阅链接, got %v", in, err)
		}
	}
	for _, in := range []string{"not a link", "ftp://example.com/x"} {
		if got, err := extractTarget(in); err == nil {
			t.Errorf("extractTarget(%q) should fail, got %q", in, got)
		} else if !strings.Contains(err.Error(), "http") {
			t.Errorf("error for %q should mention http(s), got %v", in, err)
		}
	}
}
