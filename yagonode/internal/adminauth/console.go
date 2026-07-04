package adminauth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// APIKeyInfo is a redacted view of a stored API key for the admin console; it
// never carries the secret.
type APIKeyInfo struct {
	ID         string
	Label      string
	Scopes     []Scope
	CreatedAt  time.Time
	LastUsedAt time.Time
}

// CreatedAPIKey is a freshly minted API key. Secret is the full key and is
// available only at creation; it is never recoverable afterwards.
type CreatedAPIKey struct {
	ID     string
	Secret string
	Label  string
	Scopes []Scope
}

// ErrPasswordMismatch reports that the supplied current password was incorrect.
var ErrPasswordMismatch = errors.New("current password is incorrect")

// ErrInvalidScope reports that a requested API-key scope was empty or unknown.
var ErrInvalidScope = errors.New("invalid api-key scope")

// KnownScopes returns the assignable API-key scopes in a stable display order.
func KnownScopes() []Scope {
	return []Scope{
		ScopeAdminRead,
		ScopeAdminWrite,
		ScopeCrawlWrite,
		ScopeSearchRead,
		ScopeSearchRaw,
	}
}

// PrincipalFromContext returns the authenticated admin session's username. It is
// present only for cookie-session requests, not API-key requests.
func PrincipalFromContext(ctx context.Context) (string, bool) {
	record, ok := sessionFromContext(ctx)
	if !ok || record.Username == "" {
		return "", false
	}

	return record.Username, true
}

// ListAPIKeys returns the stored API keys without their secrets.
func (s *Service) ListAPIKeys(ctx context.Context) ([]APIKeyInfo, error) {
	infos, err := s.apiKeys.list(ctx)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	out := make([]APIKeyInfo, 0, len(infos))
	for _, info := range infos {
		out = append(out, APIKeyInfo{
			ID:         info.ID,
			Label:      info.Label,
			Scopes:     info.Scopes,
			CreatedAt:  info.CreatedAt,
			LastUsedAt: info.LastUsedAt,
		})
	}

	return out, nil
}

// CreateAPIKey mints a new API key with the given label and scope names. The
// returned secret is available only once.
func (s *Service) CreateAPIKey(
	ctx context.Context,
	label string,
	scopeNames []string,
) (CreatedAPIKey, error) {
	scopes, err := parseScopes(scopeNames)
	if err != nil {
		return CreatedAPIKey{}, fmt.Errorf("%w: %w", ErrInvalidScope, err)
	}
	created, err := s.apiKeys.create(ctx, label, scopes)
	if err != nil {
		return CreatedAPIKey{}, fmt.Errorf("create api key: %w", err)
	}

	return CreatedAPIKey{
		ID:     created.ID,
		Secret: created.Key,
		Label:  created.Label,
		Scopes: created.Scopes,
	}, nil
}

// RevokeAPIKey deletes the API key with the given ID, reporting whether it existed.
func (s *Service) RevokeAPIKey(ctx context.Context, id string) (bool, error) {
	existed, err := s.apiKeys.delete(ctx, id)
	if err != nil {
		return false, fmt.Errorf("revoke api key: %w", err)
	}

	return existed, nil
}

// ChangePassword verifies the current password for username and, on success,
// replaces it with newPassword. It returns ErrPasswordMismatch when the current
// password is wrong.
func (s *Service) ChangePassword(
	ctx context.Context,
	username, currentPassword, newPassword string,
) error {
	valid, err := s.creds.verify(ctx, username, currentPassword)
	if err != nil {
		return fmt.Errorf("verify current password: %w", err)
	}
	if !valid {
		return ErrPasswordMismatch
	}
	if err := s.creds.setAdmin(ctx, username, newPassword); err != nil {
		return fmt.Errorf("set new password: %w", err)
	}

	return nil
}
