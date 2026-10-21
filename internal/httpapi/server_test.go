package httpapi

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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

func encryptTest(t *testing.T, yaml string) string {
	t.Helper()
	key, _ := hex.DecodeString(subscription.GCMKeyHex)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	iv := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(iv)
	return base64.StdEncoding.EncodeToString(append(iv, gcm.Seal(nil, iv, []byte(yaml), nil)...))
}

func TestAuthAndSourceKeys(t *testing.T) {
	yaml := "proxies:\n  - name: test-node\n    type: ss\n    server: example.com\n"
	ciphertext := encryptTest(t, yaml)
	profile := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=4")
		_, _ = w.Write([]byte(ciphertext))
	}))
	defer profile.Close()
	t.Setenv("FASTLINK_SUB_URL", profile.URL+"/local-sub")

	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	cfg.AccessKey = "panelkey"
	svc := service.New(root, cfg, st)
	svc.Fetcher = &subscription.Fetcher{Client: profile.Client(), AllowPrivate: true}

	web := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	api := New(svc, cfg, root, web, "test", nil)
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	// No auth -> 401.
	resp, err := http.Get(server.URL + "/api/v1/state")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("state without auth = %d", resp.StatusCode)
	}
	resp.Body.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	loginBody := strings.NewReader(`{"key":"panelkey"}`)
	resp, err = client.Post(server.URL+"/api/v1/login", "application/json", loginBody)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = client.Post(server.URL+"/api/v1/refresh", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var refresh struct {
		Nodes int `json:"nodes"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&refresh)
	resp.Body.Close()
	if refresh.Nodes == 0 {
		t.Fatalf("refresh nodes = 0")
	}

	resp, err = client.Get(server.URL + "/api/v1/state")
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Addresses []struct {
			URL string `json:"url"`
		} `json:"addresses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&state)
	resp.Body.Close()
	if len(state.Addresses) == 0 {
		t.Fatal("no addresses")
	}
	key := ""
	if u, err := url.Parse(state.Addresses[0].URL); err == nil {
		key = u.Query().Get("key")
	}
	if key == "" || key == "panelkey" {
		t.Fatalf("unexpected source key %q", key)
	}

	// Source key works without the panel cookie.
	resp, err = http.Get(server.URL + "/sub?key=" + key)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("source key /sub = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(server.URL + "/sub?key=wrong")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong key = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(server.URL + "/sub?key=" + key + "&url=https%3A%2F%2Fevil.test%2Fx")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("source key + url = %d", resp.StatusCode)
	}
	resp.Body.Close()
}
