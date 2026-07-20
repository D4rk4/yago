package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
)

func buildSearchScopeAuthorizer(service *adminauth.Service) tavilyapi.ScopeAuthorizer {
	return searchScopeAuthorizer{authorizer: service.APIKeyAuthorizer()}
}

func legacySearchAPIKeyFor(config nodeConfig) string {
	if config.SearchRequireAPIKey {
		return ""
	}

	return config.SearchAPIKey
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
