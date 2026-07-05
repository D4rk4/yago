package adminui

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeSecurity struct {
	view         SecurityView
	mintResult   APIKeyMintResult
	revokeResult APIKeyRevokeResult
	pwResult     PasswordChangeResult
	mintErr      error
	revokeErr    error
	pwErr        error
	mints        int
	revokes      int
	changes      int
	lastMint     APIKeyMint
	lastRevoke   APIKeyRevoke
	lastChange   PasswordChange
}

func (f *fakeSecurity) Security(context.Context) SecurityView { return f.view }

func (f *fakeSecurity) MintAPIKey(
	_ context.Context,
	mint APIKeyMint,
) (APIKeyMintResult, error) {
	f.mints++
	f.lastMint = mint

	return f.mintResult, f.mintErr
}

func (f *fakeSecurity) RevokeAPIKey(
	_ context.Context,
	revoke APIKeyRevoke,
) (APIKeyRevokeResult, error) {
	f.revokes++
	f.lastRevoke = revoke

	return f.revokeResult, f.revokeErr
}

func (f *fakeSecurity) ChangePassword(
	_ context.Context,
	change PasswordChange,
) (PasswordChangeResult, error) {
	f.changes++
	f.lastChange = change

	return f.pwResult, f.pwErr
}

func securityViewWithKey() SecurityView {
	return SecurityView{
		Keys: []APIKeyItem{{
			ID:      "abc123",
			Label:   "ci",
			Kind:    "search",
			Scopes:  []string{"search:read"},
			Created: "2026-01-01T00:00:00Z",
		}},
		ScopeGroups: []ScopeGroup{
			{
				Title:       "Node API key",
				Description: "Ops API keys.",
				Scopes:      []ScopeOption{{Value: "admin:write", Label: "admin:write"}},
			},
			{
				Title:       "Search API key (Tavily-compatible)",
				Description: "Search API keys.",
				Scopes:      []ScopeOption{{Value: "search:read", Label: "search:read"}},
			},
		},
	}
}

func TestConsoleSecurityRendersKeysAndForms(t *testing.T) {
	t.Parallel()

	console := New(Options{Security: &fakeSecurity{view: securityViewWithKey()}})
	got := do(t, console, "/admin/security")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		"API keys", "abc123", "search:read", "Revoke",
		"Create Node API key", "Create Search API key (Tavily-compatible)",
		"Ops API keys.", "Search API keys.", `name="scope"`,
		">search<", "Change password",
		`name="current"`, `name="csrf_token"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("security page missing %q", want)
		}
	}
}

func TestConsoleSecurityUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{}), "/admin/security")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, securityUnavailable) {
		t.Fatal("expected the unavailable message without a security source")
	}
}

func TestConsoleSecurityMintShowsSecretOnce(t *testing.T) {
	t.Parallel()

	security := &fakeSecurity{
		view: securityViewWithKey(),
		mintResult: APIKeyMintResult{
			OK:      true,
			Message: "API key created.",
			Created: &MintedAPIKey{ID: "new1", Secret: "yago_secret_value", Label: "bot"},
		},
	}
	console := New(Options{Security: security})

	got := doPost(t, console, "/admin/security", url.Values{
		"form":  {"mint"},
		"label": {"bot"},
		"scope": {"search:read", "admin:write"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if security.mints != 1 {
		t.Fatalf("MintAPIKey called %d times", security.mints)
	}
	if len(security.lastMint.Scopes) != 2 || security.lastMint.Label != "bot" {
		t.Fatalf("mint parsed wrong: %+v", security.lastMint)
	}
	for _, want := range []string{"yago_secret_value", "shown only once"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("mint response missing %q", want)
		}
	}
}

func TestConsoleSecurityRevokeCallsSource(t *testing.T) {
	t.Parallel()

	security := &fakeSecurity{
		view:         securityViewWithKey(),
		revokeResult: APIKeyRevokeResult{OK: true, Message: "API key revoked."},
	}
	console := New(Options{Security: security})

	got := doPost(t, console, "/admin/security", url.Values{
		"form": {"revoke"},
		"id":   {"abc123"},
	})
	if security.revokes != 1 || security.lastRevoke.ID != "abc123" {
		t.Fatalf("revoke not dispatched: calls=%d id=%q", security.revokes, security.lastRevoke.ID)
	}
	if !strings.Contains(got.body, "API key revoked.") {
		t.Fatal("revoke notice not shown")
	}
}

func TestConsoleSecurityPasswordChangeCallsSource(t *testing.T) {
	t.Parallel()

	security := &fakeSecurity{
		view:     securityViewWithKey(),
		pwResult: PasswordChangeResult{OK: true, Message: "Password changed."},
	}
	console := New(Options{Security: security})

	got := doPost(t, console, "/admin/security", url.Values{
		"form":    {"password"},
		"current": {"old"},
		"new":     {"newpass"},
		"confirm": {"newpass"},
	})
	if security.changes != 1 {
		t.Fatalf("ChangePassword called %d times", security.changes)
	}
	if security.lastChange.New != "newpass" || security.lastChange.Confirm != "newpass" {
		t.Fatalf("password change parsed wrong: %+v", security.lastChange)
	}
	if !strings.Contains(got.body, "Password changed.") {
		t.Fatal("password change notice not shown")
	}
}

func TestConsoleSecurityRejectionShowsReason(t *testing.T) {
	t.Parallel()

	security := &fakeSecurity{
		view:     securityViewWithKey(),
		pwResult: PasswordChangeResult{OK: false, Message: "The new passwords do not match."},
	}
	console := New(Options{Security: security})

	got := doPost(t, console, "/admin/security", url.Values{
		"form":    {"password"},
		"new":     {"a"},
		"confirm": {"b"},
	})
	if !strings.Contains(got.body, "The new passwords do not match.") {
		t.Fatal("rejection reason not shown")
	}
}
