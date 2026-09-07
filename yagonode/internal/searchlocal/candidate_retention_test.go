package searchlocal

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestLocalSearchRetainsStrictResultsWhenRelaxedSearchFails(t *testing.T) {
	for _, failure := range []error{context.DeadlineExceeded, errors.New("index unavailable")} {
		t.Run(failure.Error(), func(t *testing.T) {
			index := &candidateIndex{
				strict: searchindex.SearchResultSet{
					Results: []searchindex.SearchResult{{
						DocumentID: "strict", URL: "https://example.org/strict", Score: 7,
					}},
					Total: 1,
				},
				relaxErr: failure,
			}
			request := searchcore.Request{Query: "alpha beta gamma", Limit: 10}
			response, err := NewSearcher(index).Search(t.Context(), request)
			if err != nil || len(response.Results) != 1 || response.TotalResults != 1 {
				t.Fatalf("response=%+v error=%v", response, err)
			}
			if response.Results[0].DocumentID != "strict" || response.Results[0].Score != 7 ||
				len(response.PartialFailures) != 1 ||
				response.PartialFailures[0].Source != searchcore.PartialFailureSourceLocalSearch {
				t.Fatalf("retained response=%+v", response)
			}
		})
	}
}

func TestLocalSearchDistinguishesFailedRelaxationFromHonestMiss(t *testing.T) {
	request := searchcore.Request{Query: "alpha beta gamma", Limit: 10}
	for _, failure := range []error{nil, context.DeadlineExceeded} {
		index := &candidateIndex{relaxErr: failure}
		response, err := NewSearcher(index).Search(t.Context(), request)
		if !errors.Is(err, failure) || len(response.Results) != 0 ||
			len(response.PartialFailures) != 0 {
			t.Fatalf("failure=%v response=%+v error=%v", failure, response, err)
		}
	}
}
