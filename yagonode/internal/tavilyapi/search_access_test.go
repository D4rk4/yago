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
		retry    string
	}{
		{"allow", DecisionAllow, http.StatusOK, ""},
		{"unauthenticated", DecisionUnauthenticated, http.StatusUnauthorized, ""},
		{"forbidden", DecisionForbidden, http.StatusForbidden, ""},
		{"throttled", DecisionThrottled, http.StatusTooManyRequests, "1"},
		{"unavailable", DecisionUnavailable, http.StatusServiceUnavailable, "1"},
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
			if retry := rec.Header().Get("Retry-After"); retry != tc.retry {
				t.Fatalf("Retry-After = %q, want %q", retry, tc.retry)
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

func TestAnonymousSearchAllowsReadAndRejectsRawContent(t *testing.T) {
	authorizer := &stubScopeAuthorizer{decision: DecisionAllow}
	search := &fakeSearcher{}
	endpoint := NewSearchEndpointWithAccess(search, nil, SearchAccessPolicy{
		Authorizer:         authorizer,
		AllowAnonymousRead: true,
	})

	read := httptest.NewRecorder()
	readRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"anonymous smoke"}`),
	)
	endpoint.ServeHTTP(read, readRequest)
	if read.Code != http.StatusOK || search.calls != 1 || authorizer.calls != 0 {
		t.Fatalf("anonymous read status=%d searches=%d authorizations=%d", read.Code, search.calls, authorizer.calls)
	}

	raw := httptest.NewRecorder()
	rawRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"anonymous smoke","include_raw_content":true}`),
	)
	endpoint.ServeHTTP(raw, rawRequest)
	if raw.Code != http.StatusUnauthorized || search.calls != 1 || authorizer.calls != 0 {
		t.Fatalf("anonymous raw status=%d searches=%d authorizations=%d", raw.Code, search.calls, authorizer.calls)
	}
}

func TestAnonymousReadPolicyDoesNotOpenExtract(t *testing.T) {
	authorizer := &stubScopeAuthorizer{decision: DecisionAllow}
	endpoint := NewExtractEndpointWithAccess(nil, SearchAccessPolicy{
		Authorizer:         authorizer,
		AllowAnonymousRead: true,
	})
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathExtract,
		strings.NewReader(`{"urls":"https://example.com/"}`),
	)
	result := httptest.NewRecorder()
	endpoint.ServeHTTP(result, request)
	if result.Code != http.StatusUnauthorized || authorizer.calls != 0 {
		t.Fatalf("anonymous extract status=%d authorizations=%d", result.Code, authorizer.calls)
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
