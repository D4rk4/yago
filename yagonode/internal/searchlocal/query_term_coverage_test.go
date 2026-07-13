package searchlocal_test

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchlocal"
)

type queryCoverageCorpus struct {
	documents []documentstore.Document
}

func (c queryCoverageCorpus) StoredDocuments(
	_ context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	for _, document := range c.documents {
		more, err := visit(document)
		if err != nil || !more {
			return err
		}
	}

	return nil
}

func TestTwoTermSearchDoesNotReturnSingleTermNoise(t *testing.T) {
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), queryCoverageCorpus{
		documents: []documentstore.Document{
			{
				NormalizedURL: "https://complete.example/reference",
				Title:         "Complete reference",
				ExtractedText: "alpha beta",
			},
			{
				NormalizedURL: "https://first.example/noise",
				Title:         "Alpha alpha alpha",
				ExtractedText: "alpha",
			},
			{
				NormalizedURL: "https://second.example/noise",
				Title:         "Beta beta beta",
				ExtractedText: "beta",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	response, err := searchlocal.NewSearcher(index).Search(t.Context(), searchcore.Request{
		Query: "alpha beta", Terms: []string{"alpha", "beta"}, Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != 1 ||
		response.Results[0].URL != "https://complete.example/reference" {
		t.Fatalf("results = %#v", response.Results)
	}
}
