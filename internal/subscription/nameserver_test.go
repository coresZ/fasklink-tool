package subscription

import (
	"strings"
	"testing"
)

// profileWith builds a minimal profile whose dns section is the given block.
func profileWith(dns string) string {
	return "dns:\n" + dns + "\nproxies:\n  - {\"name\":\"a\",\"type\":\"vless\",\"server\":\"s\",\"port\":1}\nrules:\n  - MATCH,DIRECT\n"
}

func mustApply(t *testing.T, source string) string {
	t.Helper()
	out, err := ApplyTemplate(source)
	if err != nil {
		t.Fatalf("ApplyTemplate: %v", err)
	}
	return out
}

func TestUpstreamNameserversPrependedBeforeDefaults(t *testing.T) {
	source := profileWith(`  proxy-server-nameserver:
    - https://a.example/api-v2/tok1
    - https://b.example/api-v2/tok2
    - https://c.example/api-v2/tok3`)
	out := mustApply(t, source)

	first := strings.Index(out, "https://a.example/api-v2/tok1")
	second := strings.Index(out, "https://b.example/api-v2/tok2")
	third := strings.Index(out, "https://c.example/api-v2/tok3")
	key := strings.Index(out, "  proxy-server-nameserver:\n")
	if key < 0 {
		t.Fatal("key missing")
	}
	defaults := key + strings.Index(out[key:], "- 223.5.5.5")
	if first < 0 || second < 0 || third < 0 || defaults < key {
		t.Fatalf("missing entries: %d %d %d %d", first, second, third, defaults)
	}
	if !(first < second && second < third && third < defaults) {
		t.Fatalf("wrong order: %d %d %d %d", first, second, third, defaults)
	}
	if strings.Contains(out, proxyNSMarker) {
		t.Fatal("marker left in output")
	}
}

func TestUpstreamNameserversDedupedAgainstDefaults(t *testing.T) {
	source := profileWith(`  proxy-server-nameserver:
    - 223.5.5.5
    - 119.29.29.29
    - https://unique.example/dns-query`)
	out := mustApply(t, source)
	key := strings.Index(out, "  proxy-server-nameserver:\n")
	if key < 0 {
		t.Fatal("key missing")
	}
	end := strings.Index(out[key:], "nameserver-policy:")
	if end < 0 {
		t.Fatal("block end missing")
	}
	block := out[key : key+end]
	if got := strings.Count(block, "- 223.5.5.5"); got != 1 {
		t.Fatalf("223.5.5.5 appears %d times in block", got)
	}
	if got := strings.Count(block, "- 119.29.29.29"); got != 1 {
		t.Fatalf("119.29.29.29 appears %d times in block", got)
	}
	if !strings.Contains(out, "https://unique.example/dns-query") {
		t.Fatal("unique entry dropped")
	}
}

func TestUpstreamNameserversFiltersUnsupported(t *testing.T) {
	source := profileWith(`  proxy-server-nameserver:
    - http://plain.example/dns-query
    - not a resolver
    - tls://dot.example:853`)
	out := mustApply(t, source)
	if strings.Contains(out, "http://plain.example") {
		t.Fatal("http entry kept")
	}
	if strings.Contains(out, "not a resolver") {
		t.Fatal("garbage kept")
	}
	if !strings.Contains(out, "- tls://dot.example:853") {
		t.Fatal("tls entry dropped")
	}
}

func TestUpstreamNameserversUnquotesAndKeepsSystemAndIP(t *testing.T) {
	source := profileWith(`  proxy-server-nameserver:
    - "https://quoted.example/dns-query"
    - system
    - 9.9.9.9`)
	out := mustApply(t, source)
	if !strings.Contains(out, "- https://quoted.example/dns-query") {
		t.Fatal("quoted entry not unquoted")
	}
	if !strings.Contains(out, "- system") {
		t.Fatal("system dropped")
	}
	if !strings.Contains(out, "- 9.9.9.9") {
		t.Fatal("bare IP dropped")
	}
}

func TestUpstreamNameserversCapped(t *testing.T) {
	source := profileWith(`  proxy-server-nameserver:
    - https://one.example/a
    - https://two.example/a
    - https://three.example/a
    - https://four.example/a
    - https://five.example/a
    - https://six.example/a`)
	out := mustApply(t, source)
	if !strings.Contains(out, "https://four.example/a") {
		t.Fatal("fourth entry missing")
	}
	if strings.Contains(out, "https://five.example/a") {
		t.Fatal("cap not applied")
	}
}

func TestUpstreamNameserversBlockStopsBeforeNextKey(t *testing.T) {
	source := profileWith(`  proxy-server-nameserver:
    - https://a.example/dns-query
  nameserver-policy:
    "+.x.com": "https://dns.cloudflare.com/dns-query"`)
	out := mustApply(t, source)
	if strings.Contains(out, "- \"+.x.com\"") || !strings.Contains(out, "\"+.x.com\": \"https://dns.cloudflare.com/dns-query\"") {
		t.Fatal("block swallowed the next key or dropped it")
	}
	if !strings.Contains(out, "- https://a.example/dns-query") {
		t.Fatal("entry missing")
	}
}

func TestUpstreamNameserversFlowForm(t *testing.T) {
	source := profileWith(`  proxy-server-nameserver: [https://f1.example/a, https://f2.example/a]`)
	out := mustApply(t, source)
	if !strings.Contains(out, "- https://f1.example/a") || !strings.Contains(out, "- https://f2.example/a") {
		t.Fatal("flow form not parsed")
	}
	if strings.Contains(out, "https://f1.example/a,") {
		t.Fatal("flow form left separators")
	}
}

func TestNoUpstreamNameserversLeavesDefaultsAdjacent(t *testing.T) {
	out := mustApply(t, profileWith("  proxy-server-nameserver:\n    - 223.5.5.5\n    - 119.29.29.29"))
	if strings.Contains(out, proxyNSMarker) {
		t.Fatal("marker left in output")
	}
	idx := strings.Index(out, "  proxy-server-nameserver:\n")
	if idx < 0 {
		t.Fatal("key missing")
	}
	want := "  proxy-server-nameserver:\n    - 223.5.5.5\n    - 119.29.29.29\n"
	if !strings.HasPrefix(out[idx:], want) {
		t.Fatalf("defaults not adjacent:\n%.120s", out[idx:])
	}
	if strings.Contains(out[idx:], "\n\n    - 223.5.5.5") {
		t.Fatal("stray blank line left behind")
	}
}

func TestMergeNameserversWithoutMarkerIsNoop(t *testing.T) {
	if got := mergeNameservers("a: 1\n", []string{"9.9.9.9"}, nil); got != "a: 1\n" {
		t.Fatalf("unexpected change: %q", got)
	}
}

func TestValidNameserver(t *testing.T) {
	for _, value := range []string{"system", "1.1.1.1", "https://x/dns-query", "TLS://x:853", "quic://x", "h3://x", "udp://x"} {
		if !validNameserver(value) {
			t.Fatalf("%q should be valid", value)
		}
	}
	for _, value := range []string{"", "http://x", "has space", "ftp://x"} {
		if validNameserver(value) {
			t.Fatalf("%q should be invalid", value)
		}
	}
}
