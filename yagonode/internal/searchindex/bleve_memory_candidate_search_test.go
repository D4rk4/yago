package searchindex

import (
	"strconv"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestMemoryCandidateOnlySearchBoundsHydrationAndKeepsTotal(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for position := range 100 {
		rawURL := "https://example.test/" + strconv.Itoa(position)
		if err := index.Index(t.Context(), documentstore.Document{
			NormalizedURL: rawURL,
			Title:         "bounded candidate",
			ExtractedText: "bounded candidate body",
			Language:      "en",
		}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query: "bounded", MaxResults: 10, CandidateOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 10 || result.Total != 100 {
		t.Fatalf("candidate result = %d/%d", len(result.Results), result.Total)
	}
	filtered, err := index.Search(t.Context(), SearchRequest{
		Query: "bounded", MaxResults: 10, CandidateOnly: true, Language: "de",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Results) != 0 || filtered.Total != 0 {
		t.Fatalf("filtered candidate result = %d/%d", len(filtered.Results), filtered.Total)
	}
}
