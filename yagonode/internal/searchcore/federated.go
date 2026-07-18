package searchcore

import (
	"context"
	"errors"
	"fmt"
)

var errFederatedSearchUnavailable = errors.New("federated search unavailable")

const (
	federatedLocalSearchFailed  = "local search failed"
	federatedRemoteSearchFailed = "remote search failed"
)

type federatedSearcher struct {
	local  Searcher
	remote Searcher
}

// searchOutcome carries one branch's answer across the fan-out channel.
type searchOutcome struct {
	resp Response
	err  error
}

func NewFederatedSearcher(local Searcher, remote Searcher) Searcher {
	return federatedSearcher{local: local, remote: remote}
}

func (s federatedSearcher) Search(ctx context.Context, req Request) (Response, error) {
	if req.Source != SourceGlobal || s.remote == nil {
		resp, err := s.local.Search(ctx, req)
		if err != nil {
			resp = federatedBranchFailure(
				resp,
				PartialFailureSourceLocalSearch,
				federatedLocalSearchFailed,
			)
			resp.Request = req
			if len(resp.Results) > 0 {
				return resp, nil
			}
			if cause := context.Cause(ctx); cause != nil {
				return resp, fmt.Errorf("federated search: %w", cause)
			}

			return resp, errFederatedSearchUnavailable
		}

		return resp, nil
	}

	window := requestWindow(req)
	remoteOutcome := make(chan searchOutcome, 1)
	go func() {
		resp, err := s.remote.Search(ctx, window)
		remoteOutcome <- searchOutcome{resp: resp, err: err}
	}()
	localResp, localErr := s.local.Search(ctx, window)
	remote, remoteReady := awaitRemoteOutcome(ctx, remoteOutcome, localResp)
	if !remoteReady {
		remote.err = context.Cause(ctx)
	}
	if localErr != nil {
		localResp = federatedBranchFailure(
			localResp,
			PartialFailureSourceLocalSearch,
			federatedLocalSearchFailed,
		)
	}
	remoteResp := remote.resp
	if remote.err != nil {
		remoteResp = federatedBranchFailure(
			remoteResp,
			PartialFailureSourceRemoteYaCy,
			federatedRemoteSearchFailed,
		)
	}

	merged := FuseByReciprocalRank(
		localResp.Results,
		remoteResp.Results,
	)

	response := Response{
		Request: req,
		TotalResults: federatedBranchTotal(localResp, localErr) +
			federatedBranchTotal(remoteResp, remote.err),
		Results:         offsetResults(merged, req.Offset, rankingResultLimit(req)),
		PartialFailures: append(localResp.PartialFailures, remoteResp.PartialFailures...),
		Facets:          localResp.Facets,
	}
	if len(merged) > 0 {
		return response, nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return response, fmt.Errorf("federated search: %w", cause)
	}
	if localErr != nil {
		return response, errFederatedSearchUnavailable
	}

	return response, nil
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
