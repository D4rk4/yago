package tavilyapi

import (
	"net/http/httptest"
	"testing"
)

func TestSearchAccessPolicyAcceptsExactLegacyTokenBeforeScopedLookup(t *testing.T) {
	authorizer := &stubScopeAuthorizer{decision: DecisionUnavailable}
	request := httptest.NewRequestWithContext(t.Context(), "POST", PathSearch, nil)
	request.Header.Set("Authorization", "Bearer legacy-token")
	policy := SearchAccessPolicy{
		BearerToken: "legacy-token",
		Authorizer:  authorizer,
	}

	if got := policy.authorize(request, ScopeRaw); got != DecisionAllow {
		t.Fatalf("decision = %v, want allow", got)
	}
	if authorizer.calls != 0 {
		t.Fatalf("scoped authorizer calls = %d, want 0", authorizer.calls)
	}
}

func TestSearchAccessPolicyPreservesScopedOutcomesAfterLegacyMiss(t *testing.T) {
	for _, decision := range []AuthDecision{
		DecisionAllow,
		DecisionUnauthenticated,
		DecisionForbidden,
		DecisionThrottled,
		DecisionUnavailable,
	} {
		t.Run(authDecisionName(decision), func(t *testing.T) {
			authorizer := &stubScopeAuthorizer{decision: decision}
			request := httptest.NewRequestWithContext(t.Context(), "POST", PathSearch, nil)
			request.Header.Set("Authorization", "Bearer scoped-token")
			policy := SearchAccessPolicy{
				BearerToken: "legacy-token",
				Authorizer:  authorizer,
			}

			if got := policy.authorize(request, ScopeRead); got != decision {
				t.Fatalf("decision = %v, want %v", got, decision)
			}
			if authorizer.calls != 1 || authorizer.gotScope != ScopeRead {
				t.Fatalf(
					"scoped lookup = %d calls, scope %v",
					authorizer.calls,
					authorizer.gotScope,
				)
			}
		})
	}
}

func authDecisionName(decision AuthDecision) string {
	switch decision {
	case DecisionAllow:
		return "allow"
	case DecisionUnauthenticated:
		return "unauthenticated"
	case DecisionForbidden:
		return "forbidden"
	case DecisionThrottled:
		return "throttled"
	case DecisionUnavailable:
		return "unavailable"
	default:
		return "unknown"
	}
}
