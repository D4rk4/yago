package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
)

// searchScopeAuthorizerFor returns a scoped API-key authorizer for the Tavily
// surface when the operator requires API keys; otherwise nil, leaving the
// surface on its static-token or public policy. It reuses the admin service's
// key store, so keys minted through the operations API authenticate here.
func searchScopeAuthorizerFor(
	config nodeConfig,
	service *adminauth.Service,
) tavilyapi.ScopeAuthorizer {
	if !config.SearchRequireAPIKey {
		return nil
	}

	return searchScopeAuthorizer{authorizer: service.APIKeyAuthorizer()}
}

type searchScopeAuthorizer struct {
	authorizer *adminauth.APIKeyAuthorizer
}

func (a searchScopeAuthorizer) Authorize(
	ctx context.Context,
	token string,
	scope tavilyapi.SearchScope,
) tavilyapi.AuthDecision {
	return tavilyDecision(a.authorizer.Authorize(ctx, token, adminSearchScope(scope)))
}

func adminSearchScope(scope tavilyapi.SearchScope) adminauth.Scope {
	if scope == tavilyapi.ScopeRaw {
		return adminauth.ScopeSearchRaw
	}

	return adminauth.ScopeSearchRead
}

func tavilyDecision(outcome adminauth.APIKeyOutcome) tavilyapi.AuthDecision {
	switch outcome {
	case adminauth.APIKeyAuthorized:
		return tavilyapi.DecisionAllow
	case adminauth.APIKeyThrottled:
		return tavilyapi.DecisionThrottled
	case adminauth.APIKeyForbidden:
		return tavilyapi.DecisionForbidden
	case adminauth.APIKeyUnavailable:
		return tavilyapi.DecisionUnavailable
	case adminauth.APIKeyUnauthenticated:
		return tavilyapi.DecisionUnauthenticated
	default:
		return tavilyapi.DecisionUnauthenticated
	}
}
