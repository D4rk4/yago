package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func newTestSecuritySource(t *testing.T) *securitySource {
	t.Helper()

	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	service, err := adminauth.New(storage, adminauth.Config{})
	if err != nil {
		t.Fatalf("adminauth.New: %v", err)
	}

	return newSecuritySource(service)
}

func TestSecuritySourceMintListRevoke(t *testing.T) {
	source := newTestSecuritySource(t)
	ctx := context.Background()

	mint, err := source.MintAPIKey(ctx, adminui.APIKeyMint{
		Label:  "bot",
		Scopes: []string{"search:read", "crawl:write"},
	})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if !mint.OK || mint.Created == nil || mint.Created.Secret == "" {
		t.Fatalf("mint result = %+v", mint)
	}

	view := source.Security(ctx)
	if view.Error != "" || len(view.Keys) != 1 {
		t.Fatalf("security view = %+v", view)
	}
	if view.Keys[0].ID != mint.Created.ID || view.Keys[0].Label != "bot" ||
		len(view.Keys[0].Scopes) != 2 || view.Keys[0].Created == "" {
		t.Fatalf("listed key = %+v", view.Keys[0])
	}
	if view.Keys[0].LastUsed != "" {
		t.Fatalf("a fresh key should have no last-used time: %q", view.Keys[0].LastUsed)
	}

	revoke, err := source.RevokeAPIKey(ctx, adminui.APIKeyRevoke{ID: mint.Created.ID})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if !revoke.OK {
		t.Fatalf("revoke result = %+v", revoke)
	}
	if after := source.Security(ctx); len(after.Keys) != 0 {
		t.Fatalf("key survived revoke: %+v", after.Keys)
	}
}

func TestSecuritySourceMintRejectsNoScopes(t *testing.T) {
	source := newTestSecuritySource(t)
	result, err := source.MintAPIKey(context.Background(), adminui.APIKeyMint{Label: "x"})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if result.OK || result.Created != nil {
		t.Fatalf("expected a rejection, got %+v", result)
	}
}

func TestSecuritySourceMintRejectsUnknownScope(t *testing.T) {
	source := newTestSecuritySource(t)
	result, err := source.MintAPIKey(context.Background(), adminui.APIKeyMint{
		Label:  "x",
		Scopes: []string{"bogus"},
	})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if result.OK {
		t.Fatal("expected a rejection for an unknown scope")
	}
}

func TestSecuritySourceRevokeRejectsEmptyID(t *testing.T) {
	source := newTestSecuritySource(t)
	result, err := source.RevokeAPIKey(context.Background(), adminui.APIKeyRevoke{ID: "  "})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if result.OK {
		t.Fatal("expected a rejection for an empty id")
	}
}

func TestSecuritySourceRevokeMissing(t *testing.T) {
	source := newTestSecuritySource(t)
	result, err := source.RevokeAPIKey(context.Background(), adminui.APIKeyRevoke{ID: "nope"})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if result.OK {
		t.Fatal("expected a rejection for a missing key")
	}
}

func TestSecuritySourcePasswordValidation(t *testing.T) {
	source := newTestSecuritySource(t)
	ctx := context.Background()

	empty, _ := source.ChangePassword(ctx, adminui.PasswordChange{New: ""})
	if empty.OK {
		t.Fatal("empty new password must be rejected")
	}
	mismatch, _ := source.ChangePassword(ctx, adminui.PasswordChange{New: "a", Confirm: "b"})
	if mismatch.OK {
		t.Fatal("mismatched new passwords must be rejected")
	}
	noPrincipal, _ := source.ChangePassword(ctx, adminui.PasswordChange{New: "x", Confirm: "x"})
	if noPrincipal.OK {
		t.Fatal("a change without a signed-in admin must be rejected")
	}
}

func TestSecurityScopeGroupsCoverKnownScopesWithoutOverlap(t *testing.T) {
	groups := securityScopeGroups()
	if len(groups) != 2 {
		t.Fatalf("scope groups = %d, want node + search", len(groups))
	}
	seen := map[string]bool{}
	for _, group := range groups {
		if group.Title == "" || group.Description == "" {
			t.Fatalf("group missing title/description: %+v", group)
		}
		for _, option := range group.Scopes {
			if option.Value == "" || option.Label == "" {
				t.Fatalf("empty scope option: %+v", option)
			}
			if seen[option.Value] {
				t.Fatalf("scope %q offered in more than one group", option.Value)
			}
			seen[option.Value] = true
		}
	}
	if len(seen) != len(adminauth.KnownScopes()) {
		t.Fatalf(
			"groups offer %d scopes, want all %d known",
			len(seen),
			len(adminauth.KnownScopes()),
		)
	}
}

func TestAPIKeyKindClassifiesScopeFamilies(t *testing.T) {
	cases := map[string]struct {
		scopes []string
		want   string
	}{
		"search only": {[]string{"search:read", "search:raw"}, "search"},
		"node only":   {[]string{"admin:read", "crawl:write"}, "node"},
		"mixed":       {[]string{"search:read", "admin:write"}, "mixed"},
		"empty":       {nil, "node"},
	}
	for name, tc := range cases {
		if got := apiKeyKind(tc.scopes); got != tc.want {
			t.Fatalf("%s: kind = %q, want %q", name, got, tc.want)
		}
	}
}
