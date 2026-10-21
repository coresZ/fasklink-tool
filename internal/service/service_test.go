package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fastlink-tool/internal/config"
	"fastlink-tool/internal/model"
	"fastlink-tool/internal/store"
	"fastlink-tool/internal/subscription"
)

func encryptFixture(t *testing.T, yaml string) string {
	t.Helper()
	key, err := hex.DecodeString(subscription.GCMKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(append(iv, gcm.Seal(nil, iv, []byte(yaml), nil)...))
}

func TestLoginAccountAndFetch(t *testing.T) {
	yaml := "proxies:\n  - name: test-node\n    type: ss\n    server: example.com\n"
	ciphertext := encryptFixture(t, yaml)
	var gotPassword string
	var server *httptest.Server

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/passport/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotPassword = r.Form.Get("password")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": true, "data": map[string]string{"auth_data": "token-123"}})
	})
	mux.HandleFunc("/api/v1/user/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token-123" {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": true, "data": map[string]any{}})
	})
	mux.HandleFunc("/api/v1/user/getSubscribe", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token-123" {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": true, "data": map[string]string{"subscribe_url": server.URL + "/sub-profile"}})
	})
	mux.HandleFunc("/sub-profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token-123" {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=4")
		_, _ = w.Write([]byte(ciphertext))
	})
	server = httptest.NewTLSServer(mux)
	defer server.Close()

	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := New(root, config.Default(), st)
	svc.Transport = server.Client().Transport

	result, err := svc.LoginAccount(context.Background(), model.Account{
		ID:          "acc_test",
		Email:       "a@b.c",
		PasswordEnc: "plain:secret",
		APIOrigin:   server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPassword != "secret" {
		t.Fatalf("password = %q", gotPassword)
	}
	if !strings.Contains(result.YAML, "test-node") || !strings.Contains(result.YAML, "proxy-groups:") {
		t.Fatalf("unexpected yaml: %s", result.YAML[:min(200, len(result.YAML))])
	}
	if result.SubscriptionURL != server.URL+"/sub-profile" {
		t.Fatalf("subscription URL = %q", result.SubscriptionURL)
	}
	if result.UserInfo == nil || result.UserInfo.Total != 3 {
		t.Fatalf("userInfo = %+v", result.UserInfo)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
