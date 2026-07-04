package adminui

import "context"

// ScopeOption is an assignable API-key scope offered on the mint form.
type ScopeOption struct {
	Value string
	Label string
}

// APIKeyItem is a stored API key shown in the console; it never carries the secret.
type APIKeyItem struct {
	ID       string
	Label    string
	Scopes   []string
	Created  string
	LastUsed string
}

// MintedAPIKey is a freshly created key whose secret is shown only once.
type MintedAPIKey struct {
	ID     string
	Secret string
	Label  string
	Scopes []string
}

// SecurityView is the rendered state of the Security section.
type SecurityView struct {
	Keys   []APIKeyItem
	Scopes []ScopeOption
	Error  string
}

// APIKeyMint is a request to create an API key.
type APIKeyMint struct {
	Label  string
	Scopes []string
}

// APIKeyMintResult reports the outcome of a mint. Created is set only on success
// and carries the one-time secret.
type APIKeyMintResult struct {
	OK      bool
	Message string
	Created *MintedAPIKey
}

// APIKeyRevoke is a request to revoke an API key by ID.
type APIKeyRevoke struct {
	ID string
}

// APIKeyRevokeResult reports the outcome of a revoke.
type APIKeyRevokeResult struct {
	OK      bool
	Message string
}

// PasswordChange is a request to change the admin password.
type PasswordChange struct {
	Current string
	New     string
	Confirm string
}

// PasswordChangeResult reports the outcome of a password change.
type PasswordChangeResult struct {
	OK      bool
	Message string
}

// SecuritySource backs the Security section: API-key management and the admin
// password change. A nil source renders the section as unavailable.
type SecuritySource interface {
	Security(ctx context.Context) SecurityView
	MintAPIKey(ctx context.Context, mint APIKeyMint) (APIKeyMintResult, error)
	RevokeAPIKey(ctx context.Context, revoke APIKeyRevoke) (APIKeyRevokeResult, error)
	ChangePassword(ctx context.Context, change PasswordChange) (PasswordChangeResult, error)
}
