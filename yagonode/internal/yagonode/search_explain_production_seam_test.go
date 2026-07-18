package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

type searchExplanationRecoverySource struct {
	requests []searchcore.Request
}

func (s *searchExplanationRecoverySource) Search(
	_ context.Context,
	request searchcore.Request,
) (searchcore.Response, error) {
	s.requests = append(s.requests, request)
	if !request.Fuzzy {
		return searchcore.Response{Request: request}, nil
	}

	return searchcore.Response{
		Request: request,
		Results: []searchcore.Result{
			{URL: "https://blocked.example/", Score: 10, Source: searchcore.SourceLocal},
			{URL: "https://allowed.example/", Score: 1, Source: searchcore.SourceLocal},
		},
	}, nil
}

func TestLocalSearchExplanationUsesServingRecoveryAndFiltering(t *testing.T) {
	deny := openDenylistStore(t, map[urldenylist.Kind][]string{
		urldenylist.KindDomain: {"blocked.example"},
	})
	local := &searchExplanationRecoverySource{}
	source := assemblePublicExplanationSearcher(local, nil, publicSearchAssembly{
		denylist: deny,
	})
	endpoint := newSearchExplainEndpoint(
		&searchExplainScript{}, nil, nil, nil, deny,
	).withGlobal(source)
	response, status, err := endpoint.explanation(t.Context(), searchExplainRequest{
		Query: "alphx",
		Scope: searchcore.SourceLocal,
	})
	if err != nil || status != 200 {
		t.Fatalf("explanation = %#v, %d, %v", response, status, err)
	}
	if len(local.requests) != 2 || local.requests[0].Fuzzy || !local.requests[1].Fuzzy {
		t.Fatalf("local requests = %#v", local.requests)
	}
	if len(response.Results) != 1 || response.Results[0].URL != "https://allowed.example/" {
		t.Fatalf("results = %#v", response.Results)
	}
}
