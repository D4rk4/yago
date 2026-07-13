package searchsession

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type RecentSuccessSearcher struct {
	Inner  searchcore.Searcher
	recent RecentWindow
}

func WithRecentSuccessOnIncompleteRefresh(
	inner searchcore.Searcher,
	recent RecentWindow,
) searchcore.Searcher {
	if recent == nil {
		return inner
	}

	return RecentSuccessSearcher{Inner: inner, recent: recent}
}

func (s RecentSuccessSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	response, err := s.Inner.Search(ctx, req)
	if context.Cause(ctx) != nil || err == nil && !incompleteRefresh(response) {
		return recentSearchResult(response, err)
	}
	recent, ok := recentCoverage(s.recent, req)
	if !ok {
		return recentSearchResult(response, err)
	}

	return responseWithRefreshFailures(recent, response.PartialFailures), nil
}

func recentSearchResult(
	response searchcore.Response,
	err error,
) (searchcore.Response, error) {
	if err != nil {
		return response, fmt.Errorf("recent success search: %w", err)
	}

	return response, nil
}
