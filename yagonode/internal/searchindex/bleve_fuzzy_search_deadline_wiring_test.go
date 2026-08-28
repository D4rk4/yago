package searchindex

import (
	"context"
	"testing"

	"github.com/blevesearch/bleve/v2"
)

type bleveFuzzySearchDeadlinePageProbe struct {
	bleveIndexContract
	request *bleve.SearchRequest
}

func (probe *bleveFuzzySearchDeadlinePageProbe) SearchInContext(
	_ context.Context,
	request *bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	probe.request = request

	return &bleve.SearchResult{
		Status: &bleve.SearchStatus{Total: 1, Successful: 1},
	}, nil
}

func TestBleveRequestedHitPageAppliesSearchDeadline(t *testing.T) {
	probe := &bleveFuzzySearchDeadlinePageProbe{}
	index := &BleveDiskIndex{alias: probe}
	if _, err := index.searchLexicalCandidateHitPage(
		t.Context(),
		SearchRequest{Query: "term", Terms: []string{"term"}, Fuzzy: true},
		1,
	); err != nil {
		t.Fatal(err)
	}
	fuzzy, ok := probe.request.Query.(bleveSearchDeadlineQuery)
	if !ok {
		t.Fatalf("fuzzy query=%T", probe.request.Query)
	}
	if _, ok := fuzzy.inner.(bleveFuzzySearchDeadlineQuery); !ok {
		t.Fatalf("fuzzy inner query=%T", fuzzy.inner)
	}
	if _, err := index.searchLexicalCandidateHitPage(
		t.Context(),
		SearchRequest{Query: "term", Terms: []string{"term"}},
		1,
	); err != nil {
		t.Fatal(err)
	}
	exact, ok := probe.request.Query.(bleveSearchDeadlineQuery)
	if !ok {
		t.Fatalf("exact query=%T", probe.request.Query)
	}
	if _, ok := exact.inner.(bleveFuzzySearchDeadlineQuery); ok {
		t.Fatalf("exact inner query=%T", exact.inner)
	}
}

func TestBleveCompleteHitPageAppliesSearchDeadline(t *testing.T) {
	probe := &bleveFuzzySearchDeadlinePageProbe{}
	index := &BleveDiskIndex{alias: probe}
	if _, err := index.completeSearchPage(
		t.Context(),
		SearchRequest{Query: "term", Terms: []string{"term"}, Fuzzy: true},
		1,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	fuzzy, ok := probe.request.Query.(bleveSearchDeadlineQuery)
	if !ok {
		t.Fatalf("fuzzy query=%T", probe.request.Query)
	}
	if _, ok := fuzzy.inner.(bleveFuzzySearchDeadlineQuery); !ok {
		t.Fatalf("fuzzy inner query=%T", fuzzy.inner)
	}
	if _, err := index.completeSearchPage(
		t.Context(),
		SearchRequest{Query: "term", Terms: []string{"term"}},
		1,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	exact, ok := probe.request.Query.(bleveSearchDeadlineQuery)
	if !ok {
		t.Fatalf("exact query=%T", probe.request.Query)
	}
	if _, ok := exact.inner.(bleveFuzzySearchDeadlineQuery); ok {
		t.Fatalf("exact inner query=%T", exact.inner)
	}
}
