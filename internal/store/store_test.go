package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"fastlink-tool/internal/model"
)

func TestLegacyMigrationAndSubKeys(t *testing.T) {
	root := t.TempDir()
	run := filepath.Join(root, ".run")
	if err := os.MkdirAll(filepath.Join(run, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := map[string]any{
		"active":    "account:acc_test",
		"manualUrl": "https://example.com/sub",
		"accounts": []map[string]any{{
			"id": "acc_test", "label": "test", "email": "a@b.c",
			"password": "plain:secret", "subscriptionUrl": "https://example.com/old",
			"lastLogin": 1, "lastFetch": 2,
		}},
		"subKeys": map[string]string{"account:acc_test": "accountkey"},
	}
	raw, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(run, "accounts.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cache := model.Snapshot{Source: "account:acc_test", URL: "https://example.com/old", Label: "test", FetchedAt: 10, YAML: "proxies:\n  - name: x\n"}
	cacheRaw, _ := json.Marshal(cache)
	if err := os.WriteFile(filepath.Join(run, "cache", "account_acc_test.json"), cacheRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	accounts, err := st.Accounts()
	if err != nil || len(accounts) != 1 {
		t.Fatalf("accounts: %v %v", accounts, err)
	}
	if accounts[0].PasswordEnc != "plain:secret" || accounts[0].Email != "a@b.c" {
		t.Fatalf("account: %+v", accounts[0])
	}
	if st.ActiveSource() != "account:acc_test" || st.ManualURL() != "https://example.com/sub" {
		t.Fatalf("settings: active=%q manual=%q", st.ActiveSource(), st.ManualURL())
	}
	if key, _ := st.SubKey("account:acc_test"); key != "accountkey" {
		t.Fatalf("sub key: %q", key)
	}
	if cached, _ := st.Cache("account:acc_test"); cached == nil || cached.FetchedAt != 10 {
		t.Fatalf("cache: %+v", cached)
	}
	if matches, _ := filepath.Glob(filepath.Join(run, "accounts.json.migrated-*")); len(matches) == 0 {
		t.Fatal("expected accounts.json to be renamed with timestamp suffix")
	}

	if err := st.EnsureAllSubKeys(); err != nil {
		t.Fatal(err)
	}
	keys, _ := st.SubKeys()
	if keys["manual"] == "" || keys["account:acc_test"] == "" {
		t.Fatalf("missing generated keys: %v", keys)
	}
	if _, err := st.RotateSubKey("manual"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenMovesRetiredLocalSource(t *testing.T) {
	root := t.TempDir()
	st, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertAccount(model.Account{ID: "acc_first", Kind: model.KindPassword, Email: "a@b.c", PasswordEnc: "plain:x"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetActiveSource("local"); err != nil {
		t.Fatal(err)
	}
	st.Close()

	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.ActiveSource(); got != "account:acc_first" {
		t.Fatalf("active = %q", got)
	}
}

func TestOpenMovesRetiredLocalSourceToManual(t *testing.T) {
	root := t.TempDir()
	st, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetActiveSource("local"); err != nil {
		t.Fatal(err)
	}
	st.Close()

	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.ActiveSource(); got != "manual" {
		t.Fatalf("active = %q", got)
	}
}
