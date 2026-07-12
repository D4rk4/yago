package yagonode

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

var interactiveSearchBudget = 1800 * time.Millisecond

type interactiveBudgetSearcher struct {
	inner searchcore.Searcher
}

func withInteractiveSearchBudget(inner searchcore.Searcher) searchcore.Searcher {
	return interactiveBudgetSearcher{inner: inner}
}

func (s interactiveBudgetSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	searchCtx, cancel := context.WithTimeout(ctx, interactiveSearchBudget)
	defer cancel()

	response, err := s.inner.Search(searchCtx, req)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("interactive search: %w", err)
	}

	return response, nil
}
