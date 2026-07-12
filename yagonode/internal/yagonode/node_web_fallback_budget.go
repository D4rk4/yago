package yagonode

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

var (
	webFallbackSwarmBudget    = 800 * time.Millisecond
	webFallbackProviderBudget = 650 * time.Millisecond
)

type webFallbackSwarmBudgetSearcher struct {
	inner  searchcore.Searcher
	permit func(searchcore.Request) bool
}

func withWebFallbackSwarmBudget(
	inner searchcore.Searcher,
	config webFallbackConfig,
) searchcore.Searcher {
	if inner == nil || config.Provider != webFallbackProviderDDGS ||
		config.Privacy == webFallbackPrivacyDisabled {
		return inner
	}

	return webFallbackSwarmBudgetSearcher{
		inner:  inner,
		permit: webFallbackPermit(config.Privacy),
	}
}

func (s webFallbackSwarmBudgetSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	stageContext := ctx
	cancel := func() {}
	if s.permit(req) {
		stageContext, cancel = context.WithTimeout(ctx, webFallbackSwarmBudget)
	}
	defer cancel()

	response, err := s.inner.Search(stageContext, req)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("search swarm stage: %w", err)
	}

	return response, nil
}
