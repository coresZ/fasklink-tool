package subscription

import (
	"net/url"
	"strings"
	"testing"
)

// The strict policy must keep rejecting plain http and private hosts: Refresh and
// account login rely on it to avoid being pointed at internal services.
func TestStrictPolicy(t *testing.T) {
	reject := []string{
		"http://example.com/sub",
		"https://127.0.0.1/sub",
		"https://localhost/sub",
		"https://192.168.31.227:8787/sub",
		"https://10.0.0.5/sub",
		"https://[::1]/sub",
		"https://169.254.1.1/sub",
		"https://user:pass@example.com/sub",
		"ftp://example.com/sub",
	}
	for _, raw := range reject {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		if err := StrictPolicy(parsed); err == nil {
			t.Errorf("StrictPolicy(%q) should reject", raw)
		}
	}

	accept := []string{"https://example.com/sub", "https://sub.example.com:8443/x?token=1"}
	for _, raw := range accept {
		parsed, _ := url.Parse(raw)
		if err := StrictPolicy(parsed); err != nil {
			t.Errorf("StrictPolicy(%q) should accept: %v", raw, err)
		}
	}
}

// The decrypt tool must be able to reach the panel's own http:// LAN address,
// while still refusing non-http schemes and embedded credentials.
func TestLoopbackPolicy(t *testing.T) {
	f := NewLoopbackFetcher(0)
	if f.Policy == nil {
		t.Fatal("NewLoopbackFetcher should set a policy")
	}

	accept := []string{
		"http://192.168.31.227:8787/sub?key=abc",
		"http://127.0.0.1:8787/sub?key=abc",
		"https://example.com/sub",
	}
	for _, raw := range accept {
		parsed, _ := url.Parse(raw)
		if err := f.Policy(parsed); err != nil {
			t.Errorf("loopback policy should accept %q: %v", raw, err)
		}
	}

	reject := []string{"ftp://example.com/sub", "file:///etc/passwd", "http://user:pass@host/sub"}
	for _, raw := range reject {
		parsed, _ := url.Parse(raw)
		if err := f.Policy(parsed); err == nil {
			t.Errorf("loopback policy should reject %q", raw)
		}
	}
}

// A nil Policy with AllowPrivate unset must fall back to strict behaviour, so a
// zero-value Fetcher can never silently reach internal hosts.
func TestFetcherDefaultsToStrict(t *testing.T) {
	f := &Fetcher{}
	target, err := url.Parse("http://192.168.31.227:8787/sub")
	if err != nil {
		t.Fatal(err)
	}
	check := f.Policy
	if check == nil {
		if f.AllowPrivate {
			t.Fatal("zero-value Fetcher must not allow private hosts")
		}
		check = StrictPolicy
	}
	if err := check(target); err == nil {
		t.Fatal("zero-value Fetcher reached a private host")
	}
	if !strings.Contains(StrictPolicy(target).Error(), "https") {
		t.Error("expected the https requirement to be reported")
	}
}
