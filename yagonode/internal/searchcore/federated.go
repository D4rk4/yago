package searchcore

import (
	"context"
	"fmt"
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

	// Reciprocal Rank Fusion is the single merge point: local and remote
	// lists fuse by rank, so incomparable peer scores never need calibration
	// (SEARCH-12; Cormack et al., SIGIR 2009).
	merged := DiversifyResults(FuseByReciprocalRank(
		localResp.Results,
		remoteResp.Results,
	), req)
	OrderByDateWhenRequested(merged, req)

	return Response{
		Request:         req,
		TotalResults:    localResp.TotalResults + remoteResp.TotalResults,
		Results:         offsetResults(merged, req.Offset, req.Limit),
		PartialFailures: append(localResp.PartialFailures, remoteResp.PartialFailures...),
		// Facets describe the local corpus only; peers report no counts.
		Facets: localResp.Facets,
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

// calibratedRemoteResults maps remote scores, which the remote searcher emits
// in [0, 1] from the local ranking profile, onto the local score scale so
// neither source dominates the merge by scale alone: a perfect remote profile
// match ranks with the best local hit. Without local scores the remote order
// already stands on its own.
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
