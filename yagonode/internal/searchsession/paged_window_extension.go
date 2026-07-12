package searchsession

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func requestedSearchDepth(req searchcore.Request) int {
	if req.Offset <= 0 {
		return sessionDepth
	}
	limit := req.Limit
	if limit <= 0 {
		limit = searchcore.DefaultPublicLimit
	}
	if req.Offset >= maxSessionDepth || limit >= maxSessionDepth-req.Offset {
		return maxSessionDepth
	}
	end := req.Offset + limit
	depth := ((end + sessionDepth - 1) / sessionDepth) * sessionDepth

	return min(maxSessionDepth, max(sessionDepth, depth))
}

func (s *stableSearcher) extend(
	ctx context.Context,
	entry *session,
	req searchcore.Request,
) error {
	entry.windowMu.Lock()
	defer func() {
		retained := retainedSessionBytes(entry)
		entry.windowMu.Unlock()
		s.refreshRetention(entry, retained)
	}()
	for req.Offset < entry.total &&
		min(requestedEnd(req), entry.total) > len(entry.results) {
		targetDepth := requestedSearchDepth(req)
		if targetDepth <= entry.searchDepth {
			targetDepth = min(maxSessionDepth, entry.searchDepth+sessionDepth)
		}
		if targetDepth <= entry.searchDepth {
			entry.total = len(entry.results)

			return nil
		}
		deep := req
		deep.Offset = 0
		deep.Limit = targetDepth
		resp, err := s.inner.Search(ctx, deep)
		if err != nil {
			return fmt.Errorf("search deeper window: %w", err)
		}
		previousLength := len(entry.results)
		entry.results = appendUnseen(entry.results, resp.Results, targetDepth)
		entry.searchDepth = targetDepth
		if resp.TotalResults <= len(resp.Results) || len(entry.results) == previousLength {
			entry.total = len(entry.results)

			return nil
		}
		if len(resp.Results) > targetDepth {
			resp.Results = resp.Results[:targetDepth]
		}
		entry.total = max(len(entry.results), advertisedTotal(resp))
	}

	return nil
}

func requestedEnd(req searchcore.Request) int {
	limit := req.Limit
	if limit <= 0 {
		limit = searchcore.DefaultPublicLimit
	}
	if req.Offset >= maxSessionDepth || limit >= maxSessionDepth-req.Offset {
		return maxSessionDepth
	}

	return req.Offset + limit
}

func boundedResults(results []searchcore.Result, depth int) []searchcore.Result {
	depth = min(depth, len(results))

	return cloneSessionResults(results[:depth])
}

func advertisedTotal(resp searchcore.Response) int {
	collected := len(resp.Results)
	if resp.TotalResults <= collected {
		return collected
	}

	return min(maxSessionDepth, resp.TotalResults)
}

func appendUnseen(
	current []searchcore.Result,
	candidates []searchcore.Result,
	depth int,
) []searchcore.Result {
	results := append([]searchcore.Result(nil), current...)
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		seen[sessionResultIdentity(result)] = struct{}{}
	}
	for index := 0; index < len(candidates) && len(results) < depth; index++ {
		candidate := candidates[index]
		identity := sessionResultIdentity(candidate)
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		results = append(results, cloneSessionResult(candidate))
	}

	return results
}

func sessionResultIdentity(result searchcore.Result) string {
	if result.URLHash != "" {
		return "hash:" + result.URLHash
	}

	return "url:" + result.URL
}
