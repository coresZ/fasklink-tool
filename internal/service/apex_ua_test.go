package service

import (
	"testing"

	"fastlink-tool/internal/subscription"
)

// The panel identifies clients by the full User-Agent the app sends. The short
// `apex/<version>` form used to be enough for every Apex call; it is kept only
// as a config-fetch fallback, so nothing that talks to the panel may emit it.
func TestApexUserAgentIsFullClientString(t *testing.T) {
	cases := []struct {
		name    string
		version string
		want    string
	}{
		{"config version", "7.3.1", "ua.net.flclient.app.apex/v7.3.1"},
		{"newer config wins", "7.4.0", "ua.net.flclient.app.apex/v7.4.0"},
		{"no config yet", "", subscription.ApexUserAgent(subscription.ApexDefaultVersion())},
		// The CDN config still advertises 7.2.5 while the material in this build
		// comes from 7.3.1; reporting the older number would describe a client
		// that does not exist.
		{"stale config never goes below material", "7.2.5", subscription.ApexUserAgent(subscription.ApexDefaultVersion())},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ApexConfig{Version: tc.version}.userAgent()
			if got != tc.want {
				t.Fatalf("userAgent() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestVersionOrdering(t *testing.T) {
	cases := []struct {
		a, b string
		want string
	}{
		{"7.2.5", "7.3.1", "7.3.1"},
		{"7.3.1", "7.2.5", "7.3.1"},
		{"7.3.1", "7.3.1", "7.3.1"},
		{"7.3", "7.3.1", "7.3.1"},
		{"7.10.0", "7.9.9", "7.10.0"},
		{"", "7.3.1", "7.3.1"},
	}
	for _, tc := range cases {
		if got := newerVersion(tc.a, tc.b); got != tc.want {
			t.Fatalf("newerVersion(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}
