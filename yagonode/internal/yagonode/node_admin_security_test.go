package yagonode

import (
	"context"
	"strings"
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

	view := source.Security(ctx, adminui.SecurityAPIKeyPageRequest{})
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
	if after := source.Security(ctx, adminui.SecurityAPIKeyPageRequest{}); len(after.Keys) != 0 {
		t.Fatalf("key survived revoke: %+v", after.Keys)
	}
}

func TestSecuritySourceUsesTwentyKeyCursorPages(t *testing.T) {
	source := newTestSecuritySource(t)
	ctx := context.Background()
	for index := 0; index < 25; index++ {
		result, err := source.MintAPIKey(ctx, adminui.APIKeyMint{
			Label:  "paged",
			Scopes: []string{"admin:read"},
		})
		if err != nil || !result.OK {
			t.Fatalf("mint %d = %+v, %v", index, result, err)
		}
	}
	first := source.Security(ctx, adminui.SecurityAPIKeyPageRequest{})
	if len(first.Keys) != adminui.SecurityAPIKeyPageSize || first.APIKeyTotal != 25 ||
		first.APIKeyNextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	second := source.Security(ctx, adminui.SecurityAPIKeyPageRequest{
		Cursor: first.APIKeyNextCursor,
	})
	if len(second.Keys) != 5 || second.APIKeyTotal != 25 || second.APIKeyNextCursor != "" {
		t.Fatalf("second page = %+v", second)
	}
	seen := make(map[string]struct{}, 25)
	for _, key := range append(first.Keys, second.Keys...) {
		if _, duplicate := seen[key.ID]; duplicate {
			t.Fatalf("duplicate key %q", key.ID)
		}
		seen[key.ID] = struct{}{}
	}
}

func TestSecuritySourceRejectsMalformedPageCursor(t *testing.T) {
	view := newTestSecuritySource(t).Security(
		context.Background(),
		adminui.SecurityAPIKeyPageRequest{Cursor: "bad"},
	)
	if view.Error == "" || len(view.Keys) != 0 {
		t.Fatalf("invalid cursor view = %+v", view)
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

func TestSecuritySourceMintReportsAPIKeyCapacity(t *testing.T) {
	source := newTestSecuritySource(t)
	for attempt := 0; attempt < 1024; attempt++ {
		result, err := source.MintAPIKey(context.Background(), adminui.APIKeyMint{
			Label:  "bounded",
			Scopes: []string{"admin:read"},
		})
		if err != nil {
			t.Fatalf("mint attempt %d: %v", attempt, err)
		}
		if result.Message == "API key limit reached; revoke an existing key" {
			if result.OK || result.Created != nil {
				t.Fatalf("capacity result = %+v", result)
			}

			return
		}
	}
	t.Fatal("API key capacity was not reached within the bounded test window")
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
	if !strings.Contains(groups[1].Description, "Ordinary /search needs search:read") ||
		!strings.Contains(groups[1].Description, "/crawl, and /map need search:raw") ||
		strings.Contains(groups[1].Description, "YAGO_SEARCH_REQUIRE_API_KEY is on") {
		t.Fatalf("search key description = %q", groups[1].Description)
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
