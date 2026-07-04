package tavilyapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubScopeAuthorizer struct {
	decision AuthDecision
	gotToken string
	gotScope SearchScope
	calls    int
}

func (s *stubScopeAuthorizer) Authorize(
	_ context.Context,
	token string,
	scope SearchScope,
) AuthDecision {
	s.calls++
	s.gotToken = token
	s.gotScope = scope

	return s.decision
}

func TestSearchEndpointScopedAuthDecisions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		decision AuthDecision
		code     int
	}{
		{"allow", DecisionAllow, http.StatusOK},
		{"unauthenticated", DecisionUnauthenticated, http.StatusUnauthorized},
		{"forbidden", DecisionForbidden, http.StatusForbidden},
		{"throttled", DecisionThrottled, http.StatusTooManyRequests},
		{"unavailable", DecisionUnavailable, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authorizer := &stubScopeAuthorizer{decision: tc.decision}
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodPost,
				PathSearch,
				strings.NewReader(`{"query":"go"}`),
			)
			req.Header.Set("Authorization", "Bearer id.secret")

			NewSearchEndpointWithAccess(
				&fakeSearcher{},
				nil,
				SearchAccessPolicy{Authorizer: authorizer},
			).ServeHTTP(rec, req)

			if rec.Code != tc.code {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if authorizer.gotToken != "id.secret" {
				t.Fatalf("token = %q", authorizer.gotToken)
			}
		})
	}
}

func TestSearchEndpointScopedRequiresBearer(t *testing.T) {
	authorizer := &stubScopeAuthorizer{decision: DecisionAllow}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"go"}`),
	)

	NewSearchEndpointWithAccess(
		&fakeSearcher{},
		nil,
		SearchAccessPolicy{Authorizer: authorizer},
	).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	if authorizer.calls != 0 {
		t.Fatal("authorizer must not run without a bearer token")
	}
}

func TestSearchScopeUpgradesForRawContent(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want SearchScope
	}{
		{"read", `{"query":"go"}`, ScopeRead},
		{"raw", `{"query":"go","include_raw_content":true}`, ScopeRaw},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authorizer := &stubScopeAuthorizer{decision: DecisionForbidden}
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodPost,
				PathSearch,
				strings.NewReader(tc.body),
			)
			req.Header.Set("Authorization", "Bearer id.secret")

			NewSearchEndpointWithAccess(
				&fakeSearcher{},
				nil,
				SearchAccessPolicy{Authorizer: authorizer},
			).ServeHTTP(rec, req)

			if authorizer.gotScope != tc.want {
				t.Fatalf("scope = %v, want %v", authorizer.gotScope, tc.want)
			}
		})
	}
}

func TestExtractEndpointRequiresRawScope(t *testing.T) {
	authorizer := &stubScopeAuthorizer{decision: DecisionForbidden}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathExtract,
		strings.NewReader(`{"urls":"https://example.com/"}`),
	)
	req.Header.Set("Authorization", "Bearer id.secret")

	NewExtractEndpointWithAccess(
		nil,
		SearchAccessPolicy{Authorizer: authorizer},
	).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	if authorizer.gotScope != ScopeRaw {
		t.Fatalf("extract scope = %v, want raw", authorizer.gotScope)
	}
}
