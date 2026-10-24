package httpapi

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"fastlink-tool/internal/config"
	"fastlink-tool/internal/service"
	"fastlink-tool/internal/store"
	"fastlink-tool/internal/subscription"
)

func TestProfileFilename(t *testing.T) {
	cases := []struct {
		label, host string
		want        string
	}{
		{"giffcores@gmail.com", "glados-facility.com", `attachment; filename="giffcores@gmail.com.yaml"`},
		{"火星", "b.xlovehuawei.com", `attachment; filename="b.xlovehuawei.com.yaml"; filename*=UTF-8''%E7%81%AB%E6%98%9F.yaml`},
		{"", "", `attachment; filename="fastlink.yaml"`},
		{`quo"te`, "", `attachment; filename="quote.yaml"`},
		{"", "b.xlovehuawei.com", `attachment; filename="b.xlovehuawei.com.yaml"`},
	}
	for _, c := range cases {
		if got := profileFilename(c.label, c.host); got != c.want {
			t.Fatalf("profileFilename(%q,%q) = %q, want %q", c.label, c.host, got, c.want)
		}
	}
}

// manualServer boots an API whose manual source points at a throwaway upstream,
// so the /sub naming headers can be asserted end to end.
func manualServer(t *testing.T, upstreamURL string, transport *http.Client) (*httptest.Server, *http.Client) {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cfg := config.Default()
	cfg.AccessKey = "panelkey"
	svc := service.New(root, cfg, st)
	svc.Fetcher = &subscription.Fetcher{Client: transport, AllowPrivate: true}

	web := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	server := httptest.NewServer(New(svc, cfg, root, web, "test", nil).Handler())
	t.Cleanup(server.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.Post(server.URL+"/api/v1/login", "application/json", strings.NewReader(`{"key":"panelkey"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	body := `{"source":"manual","manualUrl":"` + upstreamURL + `"}`
	resp, err = client.Post(server.URL+"/api/v1/source", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("source = %d", resp.StatusCode)
	}
	return server, client
}

func newProfile(t *testing.T) *httptest.Server {
	t.Helper()
	profile := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=4")
		_, _ = w.Write([]byte("mixed-port: 7890\nproxies:\n  - { name: 'a', type: ss, server: x.example.com }\nproxy-groups:\n"))
	}))
	t.Cleanup(profile.Close)
	return profile
}

func TestSubSetsNamingHeaders(t *testing.T) {
	profile := newProfile(t)
	server, client := manualServer(t, profile.URL+"/sub?token=abc", profile.Client())
	resp, err := client.Get(server.URL + "/sub")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sub status %d", resp.StatusCode)
	}

	disposition := resp.Header.Get("Content-Disposition")
	if disposition != `attachment; filename="127.0.0.1.yaml"` {
		t.Fatalf("unexpected content-disposition %q", disposition)
	}
	if resp.Header.Get("Profile-Web-Page-Url") == "" {
		t.Fatal("missing profile-web-page-url")
	}
}

func TestSubNameQueryOverrides(t *testing.T) {
	profile := newProfile(t)
	server, client := manualServer(t, profile.URL+"/sub?token=abc", profile.Client())
	resp, err := client.Get(server.URL + "/sub?name=" + url.QueryEscape("我的线路"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	disposition := resp.Header.Get("Content-Disposition")
	if !strings.Contains(disposition, "filename*=UTF-8''") || !strings.Contains(disposition, "%E6%88%91%E7%9A%84%E7%BA%BF%E8%B7%AF") {
		t.Fatalf("name override ignored: %q", disposition)
	}
}

func TestSubUsesManualLabel(t *testing.T) {
	profile := newProfile(t)
	server, client := manualServer(t, profile.URL+"/sub?token=abc", profile.Client())
	body := `{"source":"manual","manualLabel":"主线路"}`
	resp, err := client.Post(server.URL+"/api/v1/source", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	got, err := client.Get(server.URL + "/sub")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	disposition := got.Header.Get("Content-Disposition")
	if !strings.Contains(disposition, "%E4%B8%BB%E7%BA%BF%E8%B7%AF") {
		t.Fatalf("manual label ignored: %q", disposition)
	}
}
