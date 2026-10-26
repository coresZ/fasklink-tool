package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fastlink-tool/internal/config"
	"fastlink-tool/internal/model"
	"fastlink-tool/internal/store"
	"fastlink-tool/internal/subscription"
)

func TestLoginAccountAndFetch(t *testing.T) {
	const profile = "proxies:\n  - name: test-node\n    type: ss\n    server: example.com\n"
	key, err := subscription.ApexProfileKey()
	if err != nil {
		t.Fatal(err)
	}
	container, err := subscription.BuildAPXS([]byte(profile), key)
	if err != nil {
		t.Fatal(err)
	}

	var gotPassword string
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/auth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotPassword = r.Form.Get("password")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"auth_data": "auth-1", "token": "key-1"}})
	})
	mux.HandleFunc("/v2/feed/access-token", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "auth-1" {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"transfer_enable": 3, "expired_at": 4}})
	})
	mux.HandleFunc("/v2/feed/download", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("access_key") != "key-1" {
			http.Error(w, "bad access key", http.StatusForbidden)
			return
		}
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=4")
		_, _ = w.Write(container)
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := New(root, config.Default(), st)
	svc.Transport = server.Client().Transport
	// Seed the resolved config so the test never reaches the CDN.
	svc.apexCfg = ApexConfig{Source: server.URL, APIBases: []string{server.URL}, SubBases: []string{server.URL}, DownloadPath: "/v2/feed/download?access_key="}
	svc.apexCfgAt = time.Now()

	// The panel account is saved first in real use; login only updates its row.
	if err := st.UpsertAccount(model.Account{
		ID:          "acc_test",
		Kind:        model.KindPassword,
		Email:       "a@b.c",
		PasswordEnc: "plain:secret",
	}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.LoginAccount(context.Background(), model.Account{
		ID:          "acc_test",
		Email:       "a@b.c",
		PasswordEnc: "plain:secret",
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
	if result.SubscriptionURL != server.URL+"/v2/feed/download?access_key=key-1" {
		t.Fatalf("subscription URL = %q", result.SubscriptionURL)
	}
	if result.Apex == nil {
		t.Fatal("apex header missing")
	}
	if result.UserInfo == nil || result.UserInfo.Total != 3 {
		t.Fatalf("userInfo = %+v", result.UserInfo)
	}
	// The download URL is persisted so the next refresh skips the login.
	account, err := st.Account("acc_test")
	if err != nil || account == nil {
		t.Fatalf("account = %v, err = %v", account, err)
	}
	if account.SubscriptionURL == "" {
		t.Fatal("subscription URL not persisted")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
