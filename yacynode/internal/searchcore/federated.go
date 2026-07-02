package searchcore

import (
	"context"
	"fmt"
	"sort"
)

type federatedSearcher struct {
	local  Searcher
	remote Searcher
}

func NewFederatedSearcher(local Searcher, remote Searcher) Searcher {
	return federatedSearcher{local: local, remote: remote}
}

func (s federatedSearcher) Search(ctx context.Context, req Request) (Response, error) {
	if req.Source != SourceGlobal || s.remote == nil {
		resp, err := s.local.Search(ctx, req)
		if err != nil {
			return Response{}, fmt.Errorf("local search: %w", err)
		}

		return resp, nil
	}

	window := requestWindow(req)
	localResp, err := s.local.Search(ctx, window)
	if err != nil {
		return Response{}, fmt.Errorf("local search: %w", err)
	}
	remoteResp, err := s.remote.Search(ctx, window)
	if err != nil {
		remoteResp = Response{
			PartialFailures: []PartialFailure{{
				Source: "remote-yacy",
				Reason: err.Error(),
			}},
		}
	}

	merged := mergeResults(localResp.Results, remoteResp.Results)

	return Response{
		Request:         req,
		TotalResults:    localResp.TotalResults + remoteResp.TotalResults,
		Results:         offsetResults(merged, req.Offset, req.Limit),
		PartialFailures: append(localResp.PartialFailures, remoteResp.PartialFailures...),
	}, nil
}

func requestWindow(req Request) Request {
	window := req
	window.Offset = 0
	window.Limit = req.Offset + req.Limit
	if window.Limit <= 0 {
		window.Limit = DefaultPublicLimit
	}

	return window
}

func mergeResults(local []Result, remote []Result) []Result {
	results := make([]Result, 0, len(local)+len(remote))
	seen := map[string]struct{}{}
	for _, result := range append(local, remote...) {
		key := resultIdentity(result)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		results = append(results, result)
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

func resultIdentity(result Result) string {
	if result.URLHash != "" {
		return "hash:" + result.URLHash
	}

	return "url:" + result.URL
}

func offsetResults(results []Result, offset int, limit int) []Result {
	if offset >= len(results) {
		return nil
	}
	end := offset + limit
	if end > len(results) {
		end = len(results)
	}

	return results[offset:end]
}
