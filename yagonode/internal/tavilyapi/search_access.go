package tavilyapi

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

// SearchScope names the capability a request needs. Reading results needs
// ScopeRead; returning full page content (a `/search` raw request or `/extract`)
// needs ScopeRaw.
type SearchScope int

const (
	ScopeRead SearchScope = iota
	ScopeRaw
)

// AuthDecision is the outcome of authorizing a request against a scope.
type AuthDecision int

const (
	DecisionAllow AuthDecision = iota
	DecisionUnauthenticated
	DecisionForbidden
	DecisionThrottled
	DecisionUnavailable
)

// ScopeAuthorizer authenticates a bearer API key and reports whether it may act
// with the given scope. It is satisfied by an adminauth-backed adapter so this
// package stays independent of the key store implementation.
type ScopeAuthorizer interface {
	Authorize(ctx context.Context, token string, scope SearchScope) AuthDecision
}

func (p SearchAccessPolicy) authorize(r *http.Request, scope SearchScope) AuthDecision {
	if p.Authorizer != nil {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			return DecisionUnauthenticated
		}

		return p.Authorizer.Authorize(r.Context(), token, scope)
	}

	// No configured credential means no access, never public access: the
	// search API returns raw page content and feeds the crawler, so an
	// operator who has not minted a key or set a static token has not opted
	// into exposing it (SEC-02).
	expected := strings.TrimSpace(p.BearerToken)
	if expected == "" {
		return DecisionUnauthenticated
	}
	got, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
		return DecisionUnauthenticated
	}

	return DecisionAllow
}

func writeAuthDecision(w http.ResponseWriter, decision AuthDecision, id string) {
	switch decision {
	case DecisionForbidden:
		writeError(w, http.StatusForbidden, "forbidden", "insufficient scope", id)
	case DecisionThrottled:
		writeError(
			w,
			http.StatusTooManyRequests,
			"rate_limited",
			"too many requests, try again later",
			id,
		)
	case DecisionUnavailable:
		writeError(
			w,
			http.StatusInternalServerError,
			"auth_unavailable",
			"authorization failed",
			id,
		)
	default:
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(
			w,
			http.StatusUnauthorized,
			"unauthorized",
			"missing or invalid bearer token",
			id,
		)
	}
}
