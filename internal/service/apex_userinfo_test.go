package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fastlink-tool/internal/config"
	"fastlink-tool/internal/model"
	"fastlink-tool/internal/store"
	"fastlink-tool/internal/subscription"
)

func TestPreferUserInfo(t *testing.T) {
	feed := &model.UserInfo{Upload: 1, Download: 2, Total: 3, Expire: 4}
	if got := preferUserInfo(nil, feed); got != feed {
		t.Fatalf("nil primary should return the fallback, got %+v", got)
	}
	if got := preferUserInfo(feed, nil); got != feed {
		t.Fatalf("nil fallback should return the primary, got %+v", got)
	}
	// Header values win; only the gaps are filled from the login snapshot.
	partial := &model.UserInfo{Upload: 10, Download: 20}
	got := preferUserInfo(partial, feed)
	if got.Upload != 10 || got.Download != 20 || got.Total != 3 || got.Expire != 4 {
		t.Fatalf("merged = %+v", got)
	}
	if partial.Total != 0 || partial.Expire != 0 {
		t.Fatalf("primary was mutated: %+v", partial)
	}
}

// The panel reports two different expiry values: /v2/feed/access-token's
// `expired_at` is shifted by a full UTC+8 offset, while the subscription
// response header carries the value the panel publishes to clients. The header
// is what has to reach the UI, otherwise the expiry date moves by one day.
func TestLoginApexPrefersSubscriptionHeaderUserinfo(t *testing.T) {
	const (
		feedExpire   = 1798054159 // what /v2/feed/access-token says
		headerExpire = 1798025359 // what Subscription-Userinfo says
	)
	profile := "proxies:\n  - name: n1\n    type: anytls\n    server: example.com\n"
	key, err := subscription.ApexProfileKey()
	if err != nil {
		t.Fatal(err)
	}
	container, err := subscription.BuildAPXS([]byte(profile), key)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/auth/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"auth_data": "auth-1", "token": "login-token"}})
	})
	mux.HandleFunc("/v2/feed/access-token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"token": "key-2", "transfer_enable": 107606966272,
			"u": 4135412992, "d": 23193257594, "expired_at": feedExpire,
		}})
	})
	mux.HandleFunc("/v2/feed/download", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Subscription-Userinfo",
			"upload=4135963817; download=23194877012; total=107606966272; expire=1798025359; reset_day=9")
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
	svc.apexCfg = ApexConfig{Source: server.URL, APIBases: []string{server.URL}, SubBases: []string{server.URL}, DownloadPath: "/v2/feed/download?access_key="}
	svc.apexCfgAt = time.Now()

	if err := st.UpsertAccount(model.Account{
		ID: "acc_test", Kind: model.KindPassword, Email: "a@b.c", PasswordEnc: "plain:secret",
	}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.LoginApex(context.Background(), model.Account{
		ID: "acc_test", Email: "a@b.c", PasswordEnc: "plain:secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.UserInfo == nil {
		t.Fatal("userInfo missing")
	}
	if result.UserInfo.Expire != headerExpire {
		t.Fatalf("expire = %d, want %d (subscription header)", result.UserInfo.Expire, headerExpire)
	}
	if result.UserInfo.Upload != 4135963817 || result.UserInfo.Download != 23194877012 {
		t.Fatalf("traffic = %+v, want the header values", result.UserInfo)
	}
	if result.UserInfo.Total != 107606966272 {
		t.Fatalf("total = %d", result.UserInfo.Total)
	}
}
