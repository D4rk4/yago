package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type effectiveWebFallbackRequestSearcher struct {
	inner  searchcore.Searcher
	config webFallbackConfig
}

func withEffectiveWebFallbackRequest(
	inner searchcore.Searcher,
	config webFallbackConfig,
) searchcore.Searcher {
	return effectiveWebFallbackRequestSearcher{inner: inner, config: config}
}

func (s effectiveWebFallbackRequestSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if req.Source == searchcore.SourceLocal {
		req.AllowWebFallback = false
	} else {
		req.AllowWebFallback = effectiveWebFallbackConsent(
			s.config,
			req.AllowWebFallback,
		)
	}
	response, err := s.inner.Search(ctx, req)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("effective web fallback request: %w", err)
	}

	return response, nil
}

func effectiveWebFallbackConsent(config webFallbackConfig, requested bool) bool {
	if config.Provider != webFallbackProviderDDGS {
		return false
	}
	switch effectiveWebFallbackPrivacy(config) {
	case webFallbackPrivacyExplicit:
		return requested
	case webFallbackPrivacyEnabled, webFallbackPrivacyAlways:
		return true
	case webFallbackPrivacyDisabled:
		return false
	default:
		return false
	}
}
