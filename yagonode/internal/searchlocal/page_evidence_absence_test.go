package searchlocal

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type rejectingPageEvidenceSource struct{}

func (rejectingPageEvidenceSource) SearchEvidence(
	context.Context,
	searchindex.SearchRequest,
	[]searchindex.SearchResult,
) ([]searchindex.SearchResult, error) {
	return nil, errors.New("unexpected evidence request")
}

func TestPageEvidenceSkipsSourceWithoutPendingLocalRows(t *testing.T) {
	searcher := NewPageEvidenceSearcher(pageEvidenceInner{response: searchcore.Response{
		Results: []searchcore.Result{
			{DocumentID: "remote", Source: searchcore.SourceRemote},
			{DocumentID: "ready", Source: searchcore.SourceLocal, EvidenceReady: true},
			{Source: searchcore.SourceLocal},
		},
	}}, rejectingPageEvidenceSource{})
	response, err := searcher.Search(t.Context(), searchcore.Request{Query: "needle"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != 3 || len(response.PartialFailures) != 0 {
		t.Fatalf("response = %#v", response)
	}
}
