package tavilyapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type allowReadAuthorizer struct{ decision AuthDecision }

func (a allowReadAuthorizer) Authorize(context.Context, string, SearchScope) AuthDecision {
	return a.decision
}

func TestAuthenticatedRead(t *testing.T) {
	withToken := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/search", nil)
	withToken.Header.Set("Authorization", "Bearer key-1")
	bare := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/search", nil)

	policy := SearchAccessPolicy{Authorizer: allowReadAuthorizer{decision: DecisionAllow}}
	if !policy.AuthenticatedRead(withToken) {
		t.Fatal("allowed key must authenticate")
	}
	if policy.AuthenticatedRead(bare) {
		t.Fatal("tokenless request must not authenticate")
	}
	denied := SearchAccessPolicy{Authorizer: allowReadAuthorizer{decision: DecisionForbidden}}
	if denied.AuthenticatedRead(withToken) {
		t.Fatal("forbidden key must not authenticate")
	}
	static := SearchAccessPolicy{BearerToken: "key-1"}
	if !static.AuthenticatedRead(withToken) {
		t.Fatal("matching static token must authenticate")
	}
	open := SearchAccessPolicy{}
	if !open.AuthenticatedRead(withToken) {
		t.Fatal("open policy with a token allows")
	}
}
