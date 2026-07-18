package yagonode

import (
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestPublicSearchExplanationUsesRequestedLocalWeights(t *testing.T) {
	index := &searchExplainScript{result: searchindex.SearchResultSet{
		Results: []searchindex.SearchResult{{
			URL: "https://weighted.example/",
		}},
	}}
	source := newPublicSearchExplanationSource(
		&localExactCountingSearcher{},
		nil,
		publicSearchAssembly{storage: nodeStorage{searchIndex: index}},
	)
	weights := searchindex.DefaultRankingWeights()
	weights.Title = 9
	response, err := source.localExplanationWithWeights(weights).Search(
		t.Context(),
		searchcore.Request{Query: "alpha", Source: searchcore.SourceLocal, Limit: 10},
	)
	if err != nil || len(response.Results) != 1 || index.got.Weights != weights {
		t.Fatalf("response = %#v, weights = %#v, error = %v", response, index.got.Weights, err)
	}
}

func TestPublicSearchExplanationWrapsRetrievalFailure(t *testing.T) {
	retrievalFailure := errors.New("retrieval failed")
	source := publicSearchExplanationSource{
		serving: &fakeSearcher{err: retrievalFailure},
	}
	_, err := source.Search(t.Context(), searchcore.Request{Query: "alpha"})
	if !errors.Is(err, retrievalFailure) ||
		!strings.Contains(err.Error(), "search explanation retrieval failed") {
		t.Fatalf("retrieval error = %v", err)
	}
}
