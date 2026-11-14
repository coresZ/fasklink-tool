package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"fastlink-tool/internal/subscription"
)

// mutableProfile is an upstream that counts requests and can change its body,
// so a test can prove /sub is served from the store rather than from upstream.
type mutableProfile struct {
	hits int64
	mu   sync.Mutex
	body string
}

func newMutableProfile(t *testing.T, yaml string) (*httptest.Server, *mutableProfile) {
	t.Helper()
	p := &mutableProfile{body: yaml}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&p.hits, 1)
		p.mu.Lock()
		body := p.body
		p.mu.Unlock()
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=4")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, p
}

func (p *mutableProfile) set(yaml string) {
	p.mu.Lock()
	p.body = yaml
	p.mu.Unlock()
}

func (p *mutableProfile) count() int64 { return atomic.LoadInt64(&p.hits) }

func subBody(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, body=%s", url, resp.StatusCode, string(raw))
	}
	return string(raw)
}

// A client download must not touch the provider: the snapshot written by the
// last refresh is what every /sub serves, byte for byte.
func TestSubServesStoredSnapshot(t *testing.T) {
	profile, upstream := newMutableProfile(t, "proxies:\n  - name: node-a\n    type: ss\n    server: a.example.com\n")
	server, client, svc := newPanel(t)
	svc.Fetcher = &subscription.Fetcher{Client: profile.Client(), AllowPrivate: true}

	code, body := postJSON(t, client, server.URL+"/api/v1/source",
		`{"source":"manual","manualUrl":"`+profile.URL+`/sub?token=x","refresh":true}`)
	if code != http.StatusOK {
		t.Fatalf("source = %d %v", code, body)
	}
	if got := upstream.count(); got != 1 {
		t.Fatalf("after explicit refresh upstream hits = %d, want 1", got)
	}
	first := subBody(t, client, server.URL+"/sub")
	if !strings.Contains(first, "node-a") {
		t.Fatalf("unexpected profile: %s", first)
	}

	// Upstream now serves a different profile; /sub must keep serving the cache.
	upstream.set("proxies:\n  - name: node-b\n    type: ss\n    server: b.example.com\n")
	for i := 0; i < 3; i++ {
		if got := subBody(t, client, server.URL+"/sub"); got != first {
			t.Fatalf("download %d changed without a refresh:\n%s", i+1, got)
		}
	}
	if got := upstream.count(); got != 1 {
		t.Fatalf("3 downloads cost %d upstream requests, want 0 more (total 1)", got-1)
	}

	// The manual refresh is what moves the cache.
	if code, body := postJSON(t, client, server.URL+"/api/v1/refresh", "{}"); code != http.StatusOK {
		t.Fatalf("refresh = %d %v", code, body)
	}
	if got := upstream.count(); got != 2 {
		t.Fatalf("upstream hits after refresh = %d, want 2", got)
	}
	updated := subBody(t, client, server.URL+"/sub")
	if !strings.Contains(updated, "node-b") || updated == first {
		t.Fatalf("refresh did not replace the snapshot: %s", updated)
	}
	if got := upstream.count(); got != 2 {
		t.Fatalf("serving the refreshed snapshot hit upstream again (%d)", got)
	}
}

// With nothing cached the download still has to work: it bootstraps through the
// single refresh funnel, and concurrent requests share that one fetch.
func TestSubBootstrapsOnceWithoutCache(t *testing.T) {
	profile, upstream := newMutableProfile(t, "proxies:\n  - name: node-a\n    type: ss\n    server: a.example.com\n")
	server, client, svc := newPanel(t)
	svc.Fetcher = &subscription.Fetcher{Client: profile.Client(), AllowPrivate: true}

	// Set the source straight on the store: no handler, so no refresh, no cache.
	if err := svc.Store.SetManualURL(profile.URL + "/sub?token=x"); err != nil {
		t.Fatal(err)
	}
	key, err := svc.Store.EnsureSubKey("manual")
	if err != nil || key == "" {
		t.Fatalf("manual key = %q (%v)", key, err)
	}

	var wg sync.WaitGroup
	bodies := make([]string, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			bodies[idx] = subBody(t, client, server.URL+"/sub?key="+key)
		}(i)
	}
	wg.Wait()

	if got := upstream.count(); got != 1 {
		t.Fatalf("4 concurrent cache-miss downloads cost %d upstream requests, want 1", got)
	}
	for i, body := range bodies {
		if !strings.Contains(body, "node-a") {
			t.Fatalf("download %d got %q", i, body)
		}
	}
}

// Bootstrapping a source that is not the active one must not rewrite the panel's
// probe: the UI would otherwise show another source's node count and timestamp.
func TestBootstrapOfOtherSourceKeepsProbe(t *testing.T) {
	profile, upstream := newMutableProfile(t, "proxies:\n  - name: node-a\n    type: ss\n    server: a.example.com\n")
	server, client, svc := newPanel(t)
	svc.Fetcher = &subscription.Fetcher{Client: profile.Client(), AllowPrivate: true}

	if code, body := postJSON(t, client, server.URL+"/api/v1/source",
		`{"source":"manual","manualUrl":"`+profile.URL+`/sub?token=x","refresh":true}`); code != http.StatusOK {
		t.Fatalf("source = %d %v", code, body)
	}
	before := svc.Probe()

	code, body := postJSON(t, client, server.URL+"/api/v1/accounts",
		`{"kind":"link","label":"side-link","subscriptionUrl":"`+profile.URL+`/sub?token=other"}`)
	if code != http.StatusOK {
		t.Fatalf("create account = %d %v", code, body)
	}
	subKeys, _ := body["subKeys"].(map[string]any)
	key := ""
	for source, value := range subKeys {
		if strings.HasPrefix(source, "account:") {
			key, _ = value.(string)
		}
	}
	if key == "" {
		t.Fatalf("account has no key: %v", body)
	}

	if got := subBody(t, client, server.URL+"/sub?key="+key); !strings.Contains(got, "node-a") {
		t.Fatalf("bootstrap download failed: %s", got)
	}
	if upstream.count() != 2 {
		t.Fatalf("upstream hits = %d, want 2 (manual refresh + one bootstrap)", upstream.count())
	}
	after := svc.Probe()
	if after.Source != before.Source || after.YAML != before.YAML || after.At != before.At || after.Nodes != before.Nodes {
		t.Fatalf("probe changed by another source's bootstrap: before=%+v after=%+v", before, after)
	}
}

// A failing refresh keeps the previous snapshot servable instead of blanking /sub.
func TestSubKeepsSnapshotWhenRefreshFails(t *testing.T) {
	profile, upstream := newMutableProfile(t, "proxies:\n  - name: node-a\n    type: ss\n    server: a.example.com\n")
	server, client, svc := newPanel(t)
	svc.Fetcher = &subscription.Fetcher{Client: profile.Client(), AllowPrivate: true}

	code, body := postJSON(t, client, server.URL+"/api/v1/source",
		`{"source":"manual","manualUrl":"`+profile.URL+`/sub?token=x","refresh":true}`)
	if code != http.StatusOK {
		t.Fatalf("source = %d %v", code, body)
	}
	before := subBody(t, client, server.URL+"/sub")

	upstream.set("")
	postJSON(t, client, server.URL+"/api/v1/refresh", "{}")
	if after := subBody(t, client, server.URL+"/sub"); after != before {
		t.Fatalf("a failed refresh replaced the snapshot:\n%s", after)
	}
}
