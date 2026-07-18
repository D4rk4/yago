package yagonode

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

// securitySource adapts the admin auth service to the console's Security section:
// API-key listing, minting and revocation, plus the admin password change.
type securitySource struct {
	service *adminauth.Service
}

func newSecuritySource(service *adminauth.Service) *securitySource {
	return &securitySource{service: service}
}

func (s *securitySource) Security(
	ctx context.Context,
	request adminui.SecurityAPIKeyPageRequest,
) adminui.SecurityView {
	page, err := s.service.ListAPIKeyPage(ctx, adminauth.APIKeyPageRequest{
		Cursor: request.Cursor,
		Limit:  adminui.SecurityAPIKeyPageSize,
	})
	if err != nil {
		return adminui.SecurityView{
			ScopeGroups: securityScopeGroups(),
			Error:       "Could not load API keys.",
		}
	}

	items := make([]adminui.APIKeyItem, 0, len(page.Keys))
	for _, info := range page.Keys {
		scopes := scopeStrings(info.Scopes)
		items = append(items, adminui.APIKeyItem{
			ID:       info.ID,
			Label:    info.Label,
			Kind:     apiKeyKind(scopes),
			Scopes:   scopes,
			Created:  formatSecurityTime(info.CreatedAt),
			LastUsed: formatSecurityTime(info.LastUsedAt),
		})
	}

	return adminui.SecurityView{
		Keys:             items,
		ScopeGroups:      securityScopeGroups(),
		APIKeyTotal:      page.Total,
		APIKeyNextCursor: page.NextCursor,
	}
}

func (s *securitySource) MintAPIKey(
	ctx context.Context,
	mint adminui.APIKeyMint,
) (adminui.APIKeyMintResult, error) {
	if len(mint.Scopes) == 0 {
		return adminui.APIKeyMintResult{Message: "Select at least one scope."}, nil
	}

	created, err := s.service.CreateAPIKey(ctx, mint.Label, mint.Scopes)
	if errors.Is(err, adminauth.ErrInvalidScope) {
		return adminui.APIKeyMintResult{Message: "The selected scopes are not valid."}, nil
	}
	if message, capacityReached := adminauth.APIKeyCapacityOperatorMessage(err); capacityReached {
		return adminui.APIKeyMintResult{Message: message}, nil
	}
	if err != nil {
		return adminui.APIKeyMintResult{}, wrapSecurityErr("create api key", err)
	}

	return adminui.APIKeyMintResult{
		OK:      true,
		Message: "API key created.",
		Created: &adminui.MintedAPIKey{
			ID:     created.ID,
			Secret: created.Secret,
			Label:  created.Label,
			Scopes: scopeStrings(created.Scopes),
		},
	}, nil
}

func (s *securitySource) RevokeAPIKey(
	ctx context.Context,
	revoke adminui.APIKeyRevoke,
) (adminui.APIKeyRevokeResult, error) {
	if strings.TrimSpace(revoke.ID) == "" {
		return adminui.APIKeyRevokeResult{Message: "No API key selected."}, nil
	}

	existed, err := s.service.RevokeAPIKey(ctx, revoke.ID)
	if err != nil {
		return adminui.APIKeyRevokeResult{}, wrapSecurityErr("revoke api key", err)
	}
	if !existed {
		return adminui.APIKeyRevokeResult{Message: "That API key no longer exists."}, nil
	}

	return adminui.APIKeyRevokeResult{OK: true, Message: "API key revoked."}, nil
}

func (s *securitySource) ChangePassword(
	ctx context.Context,
	change adminui.PasswordChange,
) (adminui.PasswordChangeResult, error) {
	if change.New == "" {
		return adminui.PasswordChangeResult{Message: "The new password cannot be empty."}, nil
	}
	if change.New != change.Confirm {
		return adminui.PasswordChangeResult{Message: "The new passwords do not match."}, nil
	}

	username, ok := adminauth.PrincipalFromContext(ctx)
	if !ok {
		return adminui.PasswordChangeResult{
			Message: "Only a signed-in admin can change the password.",
		}, nil
	}

	err := s.service.ChangePassword(ctx, username, change.Current, change.New)
	if errors.Is(err, adminauth.ErrPasswordMismatch) {
		return adminui.PasswordChangeResult{Message: "The current password is incorrect."}, nil
	}
	if err != nil {
		return adminui.PasswordChangeResult{}, wrapSecurityErr("change password", err)
	}

	return adminui.PasswordChangeResult{OK: true, Message: "Password changed."}, nil
}

// securityScopeGroups splits key creation into the two key families this node
// serves: keys for its own ops/admin API (a yago surface — upstream YaCy has no
// API-key mechanism at all, only digest admin auth) and keys for the
// Tavily-compatible search API, so an operator cannot mix the two by accident.
func securityScopeGroups() []adminui.ScopeGroup {
	return []adminui.ScopeGroup{
		{
			Title: "Node API key",
			Description: "Authenticates this node's ops API on the ops listener: " +
				"admin endpoints and crawl dispatch. These keys are yago-specific — " +
				"Java YaCy has no API keys; its admin API uses HTTP digest auth instead.",
			Scopes: scopeOptions(
				adminauth.ScopeAdminRead,
				adminauth.ScopeAdminWrite,
				adminauth.ScopeCrawlWrite,
			),
		},
		{
			Title: "Search API key (Tavily-compatible)",
			Description: "Authenticates the Tavily-compatible POST /search and " +
				"POST /extract on the public listener (Authorization: Bearer), enforced " +
				"when YAGO_SEARCH_REQUIRE_API_KEY is on. Raw page content needs search:raw.",
			Scopes: scopeOptions(adminauth.ScopeSearchRead, adminauth.ScopeSearchRaw),
		},
	}
}

func scopeOptions(scopes ...adminauth.Scope) []adminui.ScopeOption {
	options := make([]adminui.ScopeOption, 0, len(scopes))
	for _, scope := range scopes {
		options = append(options, adminui.ScopeOption{
			Value: string(scope),
			Label: string(scope),
		})
	}

	return options
}

// apiKeyKind classifies a stored key by its scopes for the console list: search
// keys serve the Tavily-compatible API, node keys the ops API, and keys carrying
// both families (mintable over the raw JSON API) read as mixed.
func apiKeyKind(scopes []string) string {
	search, node := false, false
	for _, scope := range scopes {
		if strings.HasPrefix(scope, "search:") {
			search = true
		} else {
			node = true
		}
	}
	switch {
	case search && node:
		return "mixed"
	case search:
		return "search"
	default:
		return "node"
	}
}

func scopeStrings(scopes []adminauth.Scope) []string {
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, string(scope))
	}

	return out
}

func formatSecurityTime(when time.Time) string {
	if when.IsZero() {
		return ""
	}

	return when.UTC().Format(time.RFC3339)
}

func wrapSecurityErr(action string, err error) error {
	return fmt.Errorf("%s: %w", action, err)
}
