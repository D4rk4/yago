package searchindex

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/queryidentifier"
)

func TestDiskIdentifierUsesCompleteIndexedFieldsBeforePagination(t *testing.T) {
	const base = "https://directory.example/"
	documents := []documentstore.Document{
		{NormalizedURL: base + "body", ExtractedText: strings.Repeat("Material. ", 500) + "D9‐2X"},
		{NormalizedURL: base + "heading", Headings: []string{"d9-2x reference"}},
		{NormalizedURL: base + "title", Title: "Reference for D9-2X"},
		{NormalizedURL: base + "D9-2X", Title: "Reference"},
		{NormalizedURL: base + "anchor", Inlinks: []documentstore.AnchorText{{Text: "D9-2X"}}},
		{NormalizedURL: base + "adjacent", Title: "D9 2X"},
		{NormalizedURL: base + "separate", Headings: []string{"D9-", "2X"}},
		{
			NormalizedURL: base + "unindexed",
			Title:         "D9 2X",
			Inlinks:       []documentstore.AnchorText{{Text: "D9-2X", NoFollow: true}},
		},
	}
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "index"),
		newFakeDocumentDirectory(documents...),
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	for _, path := range []string{"body", "heading", "anchor", "title"} {
		t.Run(path, func(t *testing.T) {
			result, err := index.Search(t.Context(), SearchRequest{
				Query: "d9 2x", HyphenatedIdentifier: queryidentifier.ParseHyphenated("D9-2X"),
				CandidateOnly: true, MaxResults: 1, InURL: path,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Total != 1 || len(result.Results) != 1 || result.Results[0].URL != base+path {
				t.Errorf("results = %v, total = %d", result.Results, result.Total)
			}
		})
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query: "d9 2x", HyphenatedIdentifier: queryidentifier.ParseHyphenated("D9-2X"),
		CandidateOnly: true, MaxResults: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 5 || len(result.Results) != 1 {
		t.Errorf("results = %v, total = %d", result.Results, result.Total)
	}
}
