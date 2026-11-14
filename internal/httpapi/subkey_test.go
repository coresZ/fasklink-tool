package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"fastlink-tool/internal/config"
	"fastlink-tool/internal/service"
	"fastlink-tool/internal/store"
)

// newPanel boots a logged-in panel with no web assets; enough for the key API.
func newPanel(t *testing.T) (*httptest.Server, *http.Client, *service.Service) {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := config.Default()
	cfg.AccessKey = "panelkey"
	svc := service.New(root, cfg, st)
	web := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	api := New(svc, cfg, root, web, "test", nil)
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.Post(server.URL+"/api/v1/login", "application/json", strings.NewReader(`{"key":"panelkey"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", resp.StatusCode)
	}
	return server, client, svc
}

func postJSON(t *testing.T, client *http.Client, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := client.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestSubKeyCustomAndRotate(t *testing.T) {
	server, client, svc := newPanel(t)
	if err := svc.Store.SetManualURL("https://manual.invalid/sub"); err != nil {
		t.Fatal(err)
	}
	generated, err := svc.Store.EnsureSubKey("manual")
	if err != nil || generated == "" {
		t.Fatalf("manual source should start with a generated key, got %q (%v)", generated, err)
	}

	// 1) A typed key is stored verbatim and echoed back.
	code, body := postJSON(t, client, server.URL+"/api/v1/sub-key", `{"source":"manual","key":"my-own-key"}`)
	if code != http.StatusOK {
		t.Fatalf("set key = %d %v", code, body)
	}
	if body["key"] != "my-own-key" {
		t.Errorf("response key = %v", body["key"])
	}
	if got, _ := svc.Store.SubKey("manual"); got != "my-own-key" {
		t.Errorf("persisted key = %q", got)
	}

	// 2) The new key authenticates /sub; the old one stops working.
	resp, err := http.Get(server.URL + "/sub?key=my-own-key")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("custom key should be accepted on /sub")
	}
	resp, err = http.Get(server.URL + "/sub?key=" + generated)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old key after set = %d, want 401", resp.StatusCode)
	}

	// 3) The state and accounts payloads expose the current key.
	get := func(path string, dst any) {
		resp, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			t.Fatal(err)
		}
	}
	var state struct {
		Source struct {
			Key string `json:"key"`
		} `json:"source"`
	}
	get("/api/v1/state", &state)
	if state.Source.Key != "my-own-key" {
		t.Errorf("state source.key = %q", state.Source.Key)
	}
	var accounts struct {
		SubKeys map[string]string `json:"subKeys"`
	}
	get("/api/v1/accounts", &accounts)
	if accounts.SubKeys["manual"] != "my-own-key" {
		t.Errorf("accounts subKeys = %v", accounts.SubKeys)
	}

	// 4) Bad keys are rejected with the documented messages.
	cases := map[string]string{
		`{"source":"manual","key":"abc"}`:      "6–64",
		`{"source":"manual","key":"bad key"}`:  "6–64",
		`{"source":"manual","key":"panelkey"}`: "面板口令",
		`{"source":"bogus","key":"abcdef"}`:    "数据源",
	}
	for payload, want := range cases {
		code, body := postJSON(t, client, server.URL+"/api/v1/sub-key", payload)
		if code != http.StatusBadRequest {
			t.Errorf("%s -> %d, want 400", payload, code)
			continue
		}
		msg, _ := body["error"].(string)
		if !strings.Contains(msg, want) {
			t.Errorf("%s -> %q, want it to mention %q", payload, msg, want)
		}
	}
	if got, _ := svc.Store.SubKey("manual"); got != "my-own-key" {
		t.Errorf("rejected requests must not touch the key, got %q", got)
	}

	// 5) An empty key still means "generate a new one", but only on request.
	code, body = postJSON(t, client, server.URL+"/api/v1/sub-key", `{"source":"manual"}`)
	if code != http.StatusOK {
		t.Fatalf("rotate = %d %v", code, body)
	}
	rotated, _ := body["key"].(string)
	if rotated == "" || rotated == "my-own-key" {
		t.Fatalf("rotate returned %q", rotated)
	}
	if got, _ := svc.Store.SubKey("manual"); got != rotated {
		t.Errorf("rotate persisted %q, response said %q", got, rotated)
	}
}

// The account form's optional key must reach the store without changing the
// generated key of an untouched account.
func TestAccountsSubKeyRoundTrip(t *testing.T) {
	server, client, svc := newPanel(t)

	code, body := postJSON(t, client, server.URL+"/api/v1/accounts",
		`{"kind":"link","label":"机场","subscriptionUrl":"https://a.example/sub","subKey":"custom-01"}`)
	if code != http.StatusOK {
		t.Fatalf("create = %d %v", code, body)
	}
	accounts, _ := svc.Store.Accounts()
	if len(accounts) != 1 {
		t.Fatalf("accounts = %d", len(accounts))
	}
	id := accounts[0].ID
	if got, _ := svc.Store.SubKey("account:" + id); got != "custom-01" {
		t.Fatalf("key = %q, want custom-01", got)
	}

	// Editing without a key keeps it.
	code, body = postJSON(t, client, server.URL+"/api/v1/accounts",
		`{"id":"`+id+`","kind":"link","label":"机场","subscriptionUrl":"https://a.example/sub2"}`)
	if code != http.StatusOK {
		t.Fatalf("edit = %d %v", code, body)
	}
	if got, _ := svc.Store.SubKey("account:" + id); got != "custom-01" {
		t.Errorf("edit without subKey changed the key to %q", got)
	}

	// A key owned by another source is refused before the write.
	if err := svc.Store.SetManualURL("https://manual.invalid/sub"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetSubKey("manual", "manual-key"); err != nil {
		t.Fatal(err)
	}
	code, body = postJSON(t, client, server.URL+"/api/v1/accounts",
		`{"id":"`+id+`","kind":"link","label":"机场改名","subscriptionUrl":"https://a.example/sub3","subKey":"manual-key"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("duplicate key create = %d %v", code, body)
	}
	if reloaded, _ := svc.Store.Account(id); reloaded == nil || reloaded.Label != "机场" {
		t.Error("rejected save must not rename the account")
	}
}
