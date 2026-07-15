package searchindex

import (
	"context"
	"errors"
	"testing"
)

type cachedEvidenceIndex struct {
	*countingIndex
	err error
}

func (c cachedEvidenceIndex) SearchEvidence(
	_ context.Context,
	_ SearchRequest,
	results []SearchResult,
) ([]SearchResult, error) {
	if c.err != nil {
		return nil, c.err
	}
	results[0].EvidenceReady = true

	return results, nil
}

func TestCachedSearchEvidenceForwarding(t *testing.T) {
	results := []SearchResult{{DocumentID: "document"}}
	withoutSource := NewCachedSearchIndex(&countingIndex{}, 1)
	unchanged, err := withoutSource.SearchEvidence(t.Context(), SearchRequest{}, results)
	if err != nil || unchanged[0].EvidenceReady {
		t.Fatalf("unchanged=%#v error=%v", unchanged, err)
	}

	withSource := NewCachedSearchIndex(cachedEvidenceIndex{countingIndex: &countingIndex{}}, 1)
	enriched, err := withSource.SearchEvidence(t.Context(), SearchRequest{}, results)
	if err != nil || !enriched[0].EvidenceReady {
		t.Fatalf("enriched=%#v error=%v", enriched, err)
	}

	sentinel := errors.New("evidence failed")
	failing := NewCachedSearchIndex(cachedEvidenceIndex{
		countingIndex: &countingIndex{},
		err:           sentinel,
	}, 1)
	if _, err := failing.SearchEvidence(
		t.Context(),
		SearchRequest{},
		results,
	); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
}

func TestCachedSearchEvidenceDropsRelaxedRowsWithoutInnerEvidence(t *testing.T) {
	cache := NewCachedSearchIndex(&countingIndex{}, 1)
	results := []SearchResult{
		{DocumentID: "strict", StrictRank: 1},
		{DocumentID: "relaxed", RelaxedRank: 1},
	}
	enriched, err := cache.SearchEvidence(
		t.Context(),
		SearchRequest{Relaxed: true},
		results,
	)
	if err != nil || len(enriched) != 1 || enriched[0].DocumentID != "strict" {
		t.Fatalf("enriched=%#v error=%v", enriched, err)
	}
}
