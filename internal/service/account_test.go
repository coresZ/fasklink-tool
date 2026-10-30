package service

import (
	"strings"
	"testing"

	"fastlink-tool/internal/config"
	"fastlink-tool/internal/model"
	"fastlink-tool/internal/store"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(root, config.Default(), st)
}

// A link account has no credentials and must not be rejected for a missing email.
func TestUpsertLinkAccount(t *testing.T) {
	svc := newTestService(t)

	acc, err := svc.UpsertAccount(AccountInput{
		Kind:            model.KindLink,
		Label:           "机场 A",
		SubscriptionURL: "https://sub.example-airport.com/api/v1/client/subscribe?token=abc",
	})
	if err != nil {
		t.Fatalf("create link account: %v", err)
	}
	if acc.Kind != model.KindLink {
		t.Errorf("kind = %q, want %q", acc.Kind, model.KindLink)
	}
	if acc.HasPassword || acc.PasswordEnc != "" {
		t.Error("link account must not carry credentials")
	}
	if acc.SubscriptionURL == "" {
		t.Error("link account must keep its subscription URL")
	}
	if acc.Label != "机场 A" {
		t.Errorf("label = %q, want the provided one", acc.Label)
	}

	// Reload through the store to prove the kind column round-trips.
	got, err := svc.Store.Account(acc.ID)
	if err != nil || got == nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Kind != model.KindLink {
		t.Errorf("persisted kind = %q, want link", got.Kind)
	}
}

// A link account without a URL is meaningless.
func TestUpsertLinkAccountRequiresURL(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink}); err == nil {
		t.Fatal("expected an error for a link account without a URL")
	} else if !strings.Contains(err.Error(), "订阅链接") {
		t.Errorf("error should mention 订阅链接, got %v", err)
	}
}

// Password accounts still require an email and a password on creation.
func TestUpsertPasswordAccountValidation(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.UpsertAccount(AccountInput{Kind: model.KindPassword, Password: "x"}); err == nil {
		t.Error("expected error for missing email")
	}
	if _, err := svc.UpsertAccount(AccountInput{Kind: model.KindPassword, Email: "a@b.c"}); err == nil {
		t.Error("expected error for missing password on create")
	}
	if _, err := svc.UpsertAccount(AccountInput{Kind: "bogus"}); err == nil {
		t.Error("expected error for unknown kind")
	}

	acc, err := svc.UpsertAccount(AccountInput{Kind: model.KindPassword, Email: "a@b.c", Password: "secret"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !acc.HasPassword {
		t.Error("created password account should report hasPassword")
	}
	if acc.Label != "a@b.c" {
		t.Errorf("label should default to the email, got %q", acc.Label)
	}
}

// Editing without a password keeps the stored one; editing with a password must
// invalidate the token and cached snapshot so the next fetch re-logs in.
func TestPasswordChangeClearsRuntime(t *testing.T) {
	svc := newTestService(t)

	acc, err := svc.UpsertAccount(AccountInput{Kind: model.KindPassword, Email: "a@b.c", Password: "old-pass"})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a completed login: a token and a cached profile.
	if err := svc.Store.UpsertAccount(model.Account{
		Kind:            model.KindPassword,
		ID:              acc.ID,
		Label:           acc.Label,
		Email:           acc.Email,
		PasswordEnc:     acc.PasswordEnc,
		SubscriptionURL: "https://panel.example/sub?token=old",
		LastLogin:       111,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Store.PutCache(model.Snapshot{
		Source: "account:" + acc.ID, URL: "https://panel.example/sub?token=old",
		Label: acc.Label, FetchedAt: 111, YAML: "proxies:\n  - name: x\n",
	}); err != nil {
		t.Fatal(err)
	}

	// 1) Saving without a password must preserve the runtime state.
	kept, err := svc.UpsertAccount(AccountInput{ID: acc.ID, Kind: model.KindPassword, Email: "a@b.c", Label: "renamed"})
	if err != nil {
		t.Fatalf("edit without password: %v", err)
	}
	if kept.SubscriptionURL == "" {
		t.Error("editing without a password must keep the stored subscription URL")
	}
	if cached, _ := svc.Store.Cache("account:" + acc.ID); cached == nil {
		t.Error("editing without a password must keep the cache")
	}

	// 2) Saving with a new password must clear both.
	changed, err := svc.UpsertAccount(AccountInput{ID: acc.ID, Kind: model.KindPassword, Email: "a@b.c", Password: "new-pass"})
	if err != nil {
		t.Fatalf("edit with password: %v", err)
	}
	if changed.SubscriptionURL != "" {
		t.Errorf("password change must clear subscription_url, got %q", changed.SubscriptionURL)
	}
	if changed.LastLogin != 0 {
		t.Errorf("password change must reset lastLogin, got %d", changed.LastLogin)
	}
	if cached, _ := svc.Store.Cache("account:" + acc.ID); cached != nil {
		t.Error("password change must drop the cached snapshot")
	}
}

// Changing an account's kind is rejected: the stored row would otherwise end up
// with fields that contradict its shape.
func TestKindChangeRejected(t *testing.T) {
	svc := newTestService(t)
	acc, err := svc.UpsertAccount(AccountInput{Kind: model.KindPassword, Email: "a@b.c", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpsertAccount(AccountInput{ID: acc.ID, Kind: model.KindLink, SubscriptionURL: "https://x/y"})
	if err == nil {
		t.Fatal("expected an error when changing kind")
	}
	if !strings.Contains(err.Error(), "类型") {
		t.Errorf("error should mention 类型, got %v", err)
	}
}

// Two link accounts may point at the same URL; they must not collapse into one row.
func TestLinkAccountsDoNotDedupeByEmptyEmail(t *testing.T) {
	svc := newTestService(t)
	url := "https://same.example/sub?token=abc"
	a, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, SubscriptionURL: url})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, SubscriptionURL: url})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatalf("two link accounts collapsed into one id %q", a.ID)
	}
	accounts, _ := svc.Store.Accounts()
	if len(accounts) != 2 {
		t.Fatalf("got %d accounts, want 2", len(accounts))
	}
}

// Account names must be unique across both kinds, case-insensitively, because the
// list and the subscription chips identify accounts solely by their label.
func TestAccountNameMustBeUnique(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, Label: "主线路", SubscriptionURL: "https://a.example/sub"}); err != nil {
		t.Fatalf("first: %v", err)
	}

	// Same name, different kind -> rejected.
	_, err := svc.UpsertAccount(AccountInput{Kind: model.KindPassword, Label: "主线路", Email: "u@example.com", Password: "p"})
	if err == nil {
		t.Fatal("duplicate name across kinds should be rejected")
	}
	if !strings.Contains(err.Error(), "已存在") {
		t.Errorf("error should mention 已存在, got %v", err)
	}

	// Case and surrounding spaces must not be a loophole.
	if _, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, Label: "  主线路 ", SubscriptionURL: "https://b.example/sub"}); err == nil {
		t.Error("padded duplicate should be rejected")
	}

	// Different names are fine.
	if _, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, Label: "备用线路", SubscriptionURL: "https://c.example/sub"}); err != nil {
		t.Fatalf("distinct name: %v", err)
	}
}

// Renaming an account to its own current name must not trip the uniqueness check.
func TestAccountRenameToSelfAllowed(t *testing.T) {
	svc := newTestService(t)
	acc, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, Label: "唯一", SubscriptionURL: "https://x.example/sub"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertAccount(AccountInput{ID: acc.ID, Kind: model.KindLink, Label: "唯一", SubscriptionURL: "https://x.example/sub2"}); err != nil {
		t.Fatalf("keeping the same name on edit should be allowed: %v", err)
	}
}

// Link accounts can carry a user-chosen alias; without one the label falls back
// to the URL host so the list stays readable.
func TestLinkAccountAlias(t *testing.T) {
	svc := newTestService(t)

	named, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, Label: "机场 A", SubscriptionURL: "https://sub.example-airport.com/api/v1/client/subscribe?token=abc"})
	if err != nil {
		t.Fatal(err)
	}
	if named.Label != "机场 A" {
		t.Errorf("alias should be kept, got %q", named.Label)
	}

	auto, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, SubscriptionURL: "https://auto.example.com/sub"})
	if err != nil {
		t.Fatal(err)
	}
	if auto.Label != "auto.example.com" {
		t.Errorf("label should fall back to the host, got %q", auto.Label)
	}
}

// A user-chosen key is validated before anything is written.
func TestSetSubKeyValidation(t *testing.T) {
	svc := newTestService(t)
	if err := svc.Store.SetManualURL("https://manual.example/sub"); err != nil {
		t.Fatal(err)
	}
	panelKey := config.Default().AccessKey

	bad := []struct {
		name, source, key, want string
	}{
		{"empty", "manual", "", "请填写订阅密钥"},
		{"too short", "manual", "abc12", "6–64"},
		{"too long", "manual", strings.Repeat("a", 65), "6–64"},
		{"space", "manual", "abc 123", "6–64"},
		{"slash", "manual", "abc/123", "6–64"},
		{"panel key", "manual", panelKey, "面板口令"},
		{"unknown source", "bogus", "abcdef", "未知的数据源"},
		{"missing account", "account:acc_nope", "abcdef", "账号不存在"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.SetSubKey(tc.source, tc.key)
			if err == nil {
				t.Fatalf("key %q on %q should be rejected", tc.key, tc.source)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}

	got, err := svc.SetSubKey("manual", "  my-key_01  ")
	if err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	if got != "my-key_01" {
		t.Errorf("stored key = %q, want the trimmed input", got)
	}
	if stored, _ := svc.Store.SubKey("manual"); stored != "my-key_01" {
		t.Errorf("persisted key = %q", stored)
	}

	// Two sources cannot share a key: it alone identifies the source on /sub.
	acc, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, SubscriptionURL: "https://a.example/sub"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetSubKey("account:"+acc.ID, "my-key_01"); err == nil || !strings.Contains(err.Error(), "已被其他来源使用") {
		t.Fatalf("shared key should be rejected, got %v", err)
	}
	// Re-setting a source's own key is allowed.
	if _, err := svc.SetSubKey("manual", "my-key_01"); err != nil {
		t.Fatalf("re-setting the same key: %v", err)
	}
}

// Keys must survive restarts and account edits; only an explicit set/rotate may
// change them.
func TestSubKeyStableAcrossEdits(t *testing.T) {
	svc := newTestService(t)
	acc, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, Label: "机场", SubscriptionURL: "https://a.example/sub"})
	if err != nil {
		t.Fatal(err)
	}
	source := "account:" + acc.ID
	before, err := svc.Store.SubKey(source)
	if err != nil || before == "" {
		t.Fatalf("account should get a generated key, got %q (%v)", before, err)
	}

	// Every startup runs EnsureAllSubKeys; it must be a no-op for existing keys.
	for i := 0; i < 3; i++ {
		if err := svc.Store.EnsureAllSubKeys(); err != nil {
			t.Fatal(err)
		}
	}
	if after, _ := svc.Store.SubKey(source); after != before {
		t.Errorf("EnsureAllSubKeys changed the key: %q -> %q", before, after)
	}

	// Editing the account keeps the key.
	if _, err := svc.UpsertAccount(AccountInput{ID: acc.ID, Kind: model.KindLink, Label: "机场改名", SubscriptionURL: "https://a.example/sub2"}); err != nil {
		t.Fatal(err)
	}
	if after, _ := svc.Store.SubKey(source); after != before {
		t.Errorf("editing the account changed the key: %q -> %q", before, after)
	}

	// Only an explicit change moves it.
	if _, err := svc.SetSubKey(source, "my-own-key"); err != nil {
		t.Fatal(err)
	}
	if after, _ := svc.Store.SubKey(source); after != "my-own-key" {
		t.Errorf("explicit set did not apply, got %q", after)
	}
}

// The account form can carry the key, and leaving it blank must not wipe it.
func TestUpsertAccountSubKey(t *testing.T) {
	svc := newTestService(t)

	acc, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, Label: "A", SubscriptionURL: "https://a.example/sub", SubKey: "chosen01"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Store.SubKey("account:" + acc.ID); got != "chosen01" {
		t.Fatalf("key = %q, want chosen01", got)
	}

	// An edit without a key leaves the stored one alone.
	if _, err := svc.UpsertAccount(AccountInput{ID: acc.ID, Kind: model.KindLink, Label: "A", SubscriptionURL: "https://a.example/sub2"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Store.SubKey("account:" + acc.ID); got != "chosen01" {
		t.Errorf("blank SubKey must not change the key, got %q", got)
	}

	// An edit with a key replaces it.
	if _, err := svc.UpsertAccount(AccountInput{ID: acc.ID, Kind: model.KindLink, Label: "A", SubscriptionURL: "https://a.example/sub2", SubKey: "chosen02"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Store.SubKey("account:" + acc.ID); got != "chosen02" {
		t.Errorf("key = %q, want chosen02", got)
	}

	// An invalid key rejects the whole save, leaving the account untouched.
	_, err = svc.UpsertAccount(AccountInput{ID: acc.ID, Kind: model.KindLink, Label: "改名了", SubscriptionURL: "https://a.example/sub3", SubKey: "bad key"})
	if err == nil {
		t.Fatal("invalid key should reject the save")
	}
	if reloaded, _ := svc.Store.Account(acc.ID); reloaded == nil || reloaded.Label != "A" {
		t.Error("rejected save must not touch the account row")
	}

	// A key owned by another source rejects creation outright.
	if _, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, Label: "B", SubscriptionURL: "https://b.example/sub", SubKey: "chosen02"}); err == nil || !strings.Contains(err.Error(), "已被其他来源使用") {
		t.Fatalf("duplicate key should be rejected, got %v", err)
	}
	if all, _ := svc.Store.Accounts(); len(all) != 1 {
		t.Errorf("rejected create left %d accounts, want 1", len(all))
	}
}

// Editing with the password untouched must not count as a change: the edit form
// now prefills it, so a plain rename would otherwise wipe the token and cache.
func TestSamePasswordEditKeepsRuntime(t *testing.T) {
	svc := newTestService(t)
	acc, err := svc.UpsertAccount(AccountInput{Kind: model.KindPassword, Email: "a@b.c", Password: "same-pass"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Store.UpsertAccount(model.Account{
		Kind: model.KindPassword, ID: acc.ID, Label: acc.Label, Email: acc.Email,
		PasswordEnc: acc.PasswordEnc, SubscriptionURL: "https://panel.example/sub?token=old", LastLogin: 111,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Store.PutCache(model.Snapshot{
		Source: "account:" + acc.ID, URL: "https://panel.example/sub?token=old",
		Label: acc.Label, FetchedAt: 111, YAML: "proxies:\n  - name: x\n",
	}); err != nil {
		t.Fatal(err)
	}

	kept, err := svc.UpsertAccount(AccountInput{ID: acc.ID, Kind: model.KindPassword, Email: "a@b.c", Label: "改名", Password: "same-pass"})
	if err != nil {
		t.Fatalf("edit with the same password: %v", err)
	}
	if kept.SubscriptionURL == "" {
		t.Error("resending the stored password must keep the subscription URL")
	}
	if cached, _ := svc.Store.Cache("account:" + acc.ID); cached == nil {
		t.Error("resending the stored password must keep the cache")
	}
	if plain, err := svc.AccountPassword(acc.ID); err != nil || plain != "same-pass" {
		t.Errorf("password = %q (%v), want same-pass", plain, err)
	}

	// A genuinely different password still invalidates the runtime state.
	changed, err := svc.UpsertAccount(AccountInput{ID: acc.ID, Kind: model.KindPassword, Email: "a@b.c", Password: "other-pass"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.SubscriptionURL != "" {
		t.Error("a real password change must clear the subscription URL")
	}
	if cached, _ := svc.Store.Cache("account:" + acc.ID); cached != nil {
		t.Error("a real password change must drop the cache")
	}
}

// AccountPassword feeds the edit form's prefill.
func TestAccountPassword(t *testing.T) {
	svc := newTestService(t)
	acc, err := svc.UpsertAccount(AccountInput{Kind: model.KindPassword, Email: "a@b.c", Password: "汇报口令"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := svc.AccountPassword(acc.ID); err != nil || got != "汇报口令" {
		t.Errorf("password = %q (%v), want the stored one", got, err)
	}
	if _, err := svc.AccountPassword("acc_missing"); err == nil || !strings.Contains(err.Error(), "账号不存在") {
		t.Errorf("missing account error = %v", err)
	}
	link, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, SubscriptionURL: "https://a.example/sub"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AccountPassword(link.ID); err == nil || !strings.Contains(err.Error(), "没有保存密码") {
		t.Errorf("link account error = %v", err)
	}
	// A wrong id must not leak whether the id shape is valid.
	if _, err := svc.AccountPassword(""); err == nil {
		t.Error("empty id should fail")
	}
}

// Activating an account makes it the active source.
func TestActivateAccount(t *testing.T) {
	svc := newTestService(t)
	acc, err := svc.UpsertAccount(AccountInput{Kind: model.KindLink, SubscriptionURL: "https://x.example/sub"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ActivateAccount(acc.ID); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if got := svc.Store.ActiveSource(); got != "account:"+acc.ID {
		t.Errorf("active = %q, want account:%s", got, acc.ID)
	}
	if err := svc.ActivateAccount("acc_missing"); err == nil {
		t.Error("activating a missing account should fail")
	}
}
