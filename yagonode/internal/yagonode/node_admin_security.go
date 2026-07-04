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

func (s *securitySource) Security(ctx context.Context) adminui.SecurityView {
	infos, err := s.service.ListAPIKeys(ctx)
	if err != nil {
		return adminui.SecurityView{
			Scopes: securityScopeOptions(),
			Error:  "Could not load API keys.",
		}
	}

	items := make([]adminui.APIKeyItem, 0, len(infos))
	for _, info := range infos {
		items = append(items, adminui.APIKeyItem{
			ID:       info.ID,
			Label:    info.Label,
			Scopes:   scopeStrings(info.Scopes),
			Created:  formatSecurityTime(info.CreatedAt),
			LastUsed: formatSecurityTime(info.LastUsedAt),
		})
	}

	return adminui.SecurityView{Keys: items, Scopes: securityScopeOptions()}
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

func securityScopeOptions() []adminui.ScopeOption {
	scopes := adminauth.KnownScopes()
	options := make([]adminui.ScopeOption, 0, len(scopes))
	for _, scope := range scopes {
		options = append(options, adminui.ScopeOption{
			Value: string(scope),
			Label: string(scope),
		})
	}

	return options
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
