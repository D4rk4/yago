package searchlocal

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type failingCandidateEvidenceIndex struct {
	candidateIndex
	err error
}

func (f *failingCandidateEvidenceIndex) SearchEvidence(
	context.Context,
	searchindex.SearchRequest,
	[]searchindex.SearchResult,
) ([]searchindex.SearchResult, error) {
	return nil, f.err
}

func TestSearchCandidatesReturnsEvidenceFailure(t *testing.T) {
	sentinel := errors.New("evidence failed")
	index := &failingCandidateEvidenceIndex{
		candidateIndex: candidateIndex{strict: searchindex.SearchResultSet{
			Results: []searchindex.SearchResult{{DocumentID: "document"}},
		}},
		err: sentinel,
	}
	_, err := (localSearcher{index: index}).searchCandidates(
		t.Context(),
		searchindex.SearchRequest{Query: "single", MaxResults: 1},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
}

func TestSearchCandidatesLimitsStrictResults(t *testing.T) {
	index := &candidateIndex{strict: searchindex.SearchResultSet{
		Results: []searchindex.SearchResult{
			{DocumentID: "first"},
			{DocumentID: "second"},
		},
	}}
	set, err := (localSearcher{index: index}).searchCandidates(
		t.Context(),
		searchindex.SearchRequest{Query: "single", MaxResults: 1},
	)
	if err != nil {
		t.Fatalf("searchCandidates: %v", err)
	}
	if len(set.Results) != 1 || set.Results[0].DocumentID != "first" {
		t.Fatalf("results = %#v", set.Results)
	}
}
