package adminauth

import "context"

// APIKeyOutcome is the result of authorizing a bearer API key against a scope.
type APIKeyOutcome int

const (
	APIKeyAuthorized APIKeyOutcome = iota
	APIKeyUnauthenticated
	APIKeyThrottled
	APIKeyForbidden
	APIKeyUnavailable
)

// APIKeyAuthorizer authenticates bearer API keys on non-admin surfaces such as
// the public Tavily-compatible API. Unlike Guard it never falls back to a
// session cookie: a request without a valid API key is unauthenticated. It
// shares the owning service's key store, per-key rate limiter, and audit
// observer, so keys minted through the operations API authenticate here and the
// same rate limit and audit events apply.
type APIKeyAuthorizer struct {
	keys     *apiKeyStore
	limiter  *apiKeyRateLimiter
	observer AuthObserver
}

// APIKeyAuthorizer returns a bearer-only scope authorizer backed by this
// service's key store, so a non-admin surface can authorize scoped API keys
// without a second key store over the same vault.
func (s *Service) APIKeyAuthorizer() *APIKeyAuthorizer {
	return &APIKeyAuthorizer{keys: s.apiKeys, limiter: s.keyLimiter, observer: s.observer}
}

func (a *APIKeyAuthorizer) Authorize(
	ctx context.Context,
	token string,
	required Scope,
) APIKeyOutcome {
	_, _, ok := parseAPIKey(token)
	if !ok {
		a.observer.APIKeyRejected()

		return APIKeyUnauthenticated
	}
	release, admitted := acquireAPIKeyAuthentication()
	if !admitted {
		return APIKeyUnavailable
	}
	defer release()

	info, ok, err := a.keys.authenticate(ctx, token)
	if err != nil {
		return APIKeyUnavailable
	}
	if !ok {
		a.observer.APIKeyRejected()

		return APIKeyUnauthenticated
	}
	if !a.limiter.allow(info.ID) {
		a.observer.APIKeyThrottled()

		return APIKeyThrottled
	}
	if err := a.keys.touchLastUsed(ctx, info.ID); err != nil {
		return APIKeyUnavailable
	}
	if !info.hasScope(required) {
		a.observer.APIKeyForbidden()

		return APIKeyForbidden
	}

	return APIKeyAuthorized
}
