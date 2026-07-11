package rankingtrain

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

type scriptedSearcher struct {
	results     map[string][]searchcore.Result
	failure     error
	afterSearch func()
	requests    []searchcore.Request
}

func (s *scriptedSearcher) Search(
	ctx context.Context,
	request searchcore.Request,
) (searchcore.Response, error) {
	if err := ctx.Err(); err != nil {
		return searchcore.Response{}, fmt.Errorf("scripted search context: %w", err)
	}
	s.requests = append(s.requests, request)
	if s.failure != nil {
		return searchcore.Response{}, s.failure
	}
	if s.afterSearch != nil {
		s.afterSearch()
	}

	return searchcore.Response{
		Request: request,
		Results: slices.Clone(s.results[request.Query]),
	}, nil
}

func rankingFixture() ([]searcheval.Judgment, *scriptedSearcher) {
	const queryTotal = 120
	judgments := make([]searcheval.Judgment, queryTotal)
	searcher := &scriptedSearcher{results: make(map[string][]searchcore.Result, queryTotal)}
	for index := range queryTotal {
		query := fmt.Sprintf("query %03d", index)
		badURL := fmt.Sprintf("https://bad-%03d.example/document", index)
		middleURL := fmt.Sprintf("https://middle-%03d.example/document", index)
		goodURL := fmt.Sprintf("https://good-%03d.example/document", index)
		judgments[index] = searcheval.Judgment{
			Query: query,
			Relevant: map[string]int{
				middleURL: 1,
				goodURL:   3,
			},
		}
		searcher.results[query] = []searchcore.Result{
			rankingFixtureResult(badURL, 3, 0),
			rankingFixtureResult(middleURL, 2, 0.5),
			rankingFixtureResult(goodURL, 1, 1),
		}
	}

	return judgments, searcher
}

func rankingFixtureResult(url string, score, quality float64) searchcore.Result {
	return searchcore.Result{
		URL:   url,
		Host:  url,
		Score: score,
		Evidence: searchcore.NewRankingEvidence(searchcore.RankingSignalValue{
			Signal: searchcore.SignalQuality,
			Value:  quality,
		}),
	}
}

func assertTrainingRequests(t *testing.T, requests []searchcore.Request, total int) {
	t.Helper()
	if len(requests) != total {
		t.Fatalf("requests = %d, want %d", len(requests), total)
	}
	for index, request := range requests {
		wantQuery := fmt.Sprintf("query %03d", index)
		if request.Query != wantQuery || request.Limit != MaximumCandidatesPerQuery ||
			request.Explain || len(request.Terms) == 0 {
			t.Fatalf("request %d = %+v", index, request)
		}
	}
}
