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
