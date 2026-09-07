package yagonode

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type unfinishedRelaxationIndex struct {
	stubSearchIndex
}

func (unfinishedRelaxationIndex) Search(
	ctx context.Context,
	request searchindex.SearchRequest,
) (searchindex.SearchResultSet, error) {
	if request.Relaxed {
		<-ctx.Done()
		return searchindex.SearchResultSet{}, fmt.Errorf("relaxation: %w", ctx.Err())
	}
	return searchindex.SearchResultSet{
		Total: 1,
		Results: []searchindex.SearchResult{{
			DocumentID: "https://example.org/strict", URL: "https://example.org/strict",
			Title: "Alpha beta gamma", Snippet: "Alpha beta gamma reference", Score: 7,
		}},
	}, nil
}

func (unfinishedRelaxationIndex) SearchEvidence(
	_ context.Context,
	_ searchindex.SearchRequest,
	results []searchindex.SearchResult,
) ([]searchindex.SearchResult, error) {
	return results, nil
}

func TestPublicSearchKeepsStrictCandidatesAfterRelaxationDeadline(t *testing.T) {
	previous := webFallbackExactStageBudget
	webFallbackExactStageBudget = 100 * time.Millisecond
	t.Cleanup(func() { webFallbackExactStageBudget = previous })
	var webCalls atomic.Int32
	client := &http.Client{
		Transport: fallbackRoundTrip(func(*http.Request) (*http.Response, error) {
			webCalls.Add(1)
			return nil, fmt.Errorf("provider unavailable")
		}),
	}
	assembly := productionShapeSearchAssembly(client)
	assembly.webFallback.Backend = "bing"
	index := unfinishedRelaxationIndex{}
	assembly.storage.searchIndex = index
	search := assemblePublicSearcher(
		newLocalRankingSearcher(index, nil, nil),
		productionShapeSwarmMiss{},
		assembly,
	)
	response, err := search.Search(t.Context(), searchcore.Request{
		Query: "alpha beta gamma", Source: searchcore.SourceGlobal, Limit: 1,
	})
	if err != nil || len(response.Results) != 1 || !response.Results[0].StoredLocally() ||
		response.Results[0].URL != "https://example.org/strict" || len(response.PartialFailures) == 0 ||
		webCalls.Load() != 1 {
		t.Fatalf(
			"results=%+v failures=%+v web calls=%d error=%v",
			response.Results,
			response.PartialFailures,
			webCalls.Load(),
			err,
		)
	}
}
