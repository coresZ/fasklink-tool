package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// The edit form asks for the password on demand; the accounts list must not
// carry it, and the endpoint must stay behind panel auth.
func TestAccountPasswordEndpoint(t *testing.T) {
	server, client, svc := newPanel(t)

	code, body := postJSON(t, client, server.URL+"/api/v1/accounts",
		`{"kind":"password","label":"密码号","email":"p@example.com","password":"p@ss-word"}`)
	if code != http.StatusOK {
		t.Fatalf("create = %d %v", code, body)
	}
	accounts, _ := svc.Store.Accounts()
	if len(accounts) != 1 {
		t.Fatalf("accounts = %d", len(accounts))
	}
	id := accounts[0].ID

	// The list payload never contains the password.
	resp, err := client.Get(server.URL + "/api/v1/accounts")
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 4096)
	n, _ := resp.Body.Read(raw)
	resp.Body.Close()
	if strings.Contains(string(raw[:n]), "p@ss-word") {
		t.Fatal("accounts payload must not contain the plaintext password")
	}

	code, body = postJSON(t, client, server.URL+"/api/v1/account/password", `{"id":"`+id+`"}`)
	if code != http.StatusOK {
		t.Fatalf("password = %d %v", code, body)
	}
	if body["password"] != "p@ss-word" {
		t.Errorf("password = %v", body["password"])
	}

	// Unknown id and empty id are rejected, not silently empty.
	code, body = postJSON(t, client, server.URL+"/api/v1/account/password", `{"id":"acc_nope"}`)
	if code != http.StatusBadRequest {
		t.Errorf("unknown id = %d %v", code, body)
	}
	code, body = postJSON(t, client, server.URL+"/api/v1/account/password", `{}`)
	if code != http.StatusBadRequest {
		t.Errorf("empty id = %d %v", code, body)
	}

	// Without the panel cookie the endpoint is closed.
	resp, err = http.Post(server.URL+"/api/v1/account/password", "application/json", strings.NewReader(`{"id":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d, want 401", resp.StatusCode)
	}

	// Saving the prefilled password unchanged keeps the runtime state.
	if err := svc.Store.UpdateAccountRuntime(id, "https://panel.example/sub?token=old", 111, 0, nil, ""); err != nil {
		t.Fatal(err)
	}
	code, body = postJSON(t, client, server.URL+"/api/v1/accounts",
		`{"id":"`+id+`","kind":"password","label":"密码号","email":"p@example.com","password":"p@ss-word"}`)
	if code != http.StatusOK {
		t.Fatalf("resave = %d %v", code, body)
	}
	reloaded, _ := svc.Store.Account(id)
	if reloaded == nil || reloaded.SubscriptionURL == "" {
		t.Error("resending the same password must keep the subscription URL")
	}
}
