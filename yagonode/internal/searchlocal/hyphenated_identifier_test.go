package searchlocal

import (
	"slices"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestLocalHyphenatedIdentifier(t *testing.T) {
	const base = "https://index.example/"
	documents := []documentstore.Document{
		{NormalizedURL: base + "title", Title: "Module (r8-3j)"},
		{
			NormalizedURL: base + "body",
			ExtractedText: strings.Repeat("Reference material. ", 200) + "R8-3J",
		},
		{NormalizedURL: base + "R8-3J"},
		{NormalizedURL: base + "scattered", Title: "R8 service notes for 3J"},
		{NormalizedURL: base + "reversed", Title: "3J-R8"},
		{NormalizedURL: base + "independent", Title: "R8 3J"},
		{NormalizedURL: base + "fields", Title: "R8", ExtractedText: "3J"},
		{NormalizedURL: base + "prefix", Title: "R8-3JX", ExtractedText: "3J"},
		{NormalizedURL: base + "suffix", Title: "XR8-3J", ExtractedText: "R8"},
	}
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	for _, document := range documents {
		if err := index.Index(t.Context(), document); err != nil {
			t.Fatal(err)
		}
	}
	searcher := NewSearcher(searchindex.NewCachedSearchIndex(index, 16))
	for _, test := range []struct {
		query string
		want  []string
	}{
		{"R8-3J", []string{"R8-3J", "body", "title"}},
		{"R8 3J", []string{"R8-3J", "body", "fields", "independent", "prefix", "reversed", "scattered", "suffix", "title"}},
		{"R8-5J", nil},
	} {
		t.Run(test.query, func(t *testing.T) {
			req := searchcore.RequestWithParsedQuery(
				searchcore.Request{Query: test.query, Limit: 20},
			)
			result, err := searcher.Search(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, result := range result.Results {
				paths = append(paths, strings.TrimPrefix(result.URL, base))
			}
			slices.Sort(paths)
			if !slices.Equal(paths, test.want) || result.TotalResults != len(test.want) ||
				len(result.PartialFailures) != 0 {
				t.Errorf(
					"paths = %v, total = %d, failures = %v, want %v",
					paths,
					result.TotalResults,
					result.PartialFailures,
					test.want,
				)
			}
		})
	}
}
