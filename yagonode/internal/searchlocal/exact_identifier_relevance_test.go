package searchlocal

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestRelaxedSearchRequiresMixedAlphanumericIdentifier(t *testing.T) {
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://reference.example/power-unit",
			Title:         "ZX900Q wall mounted backup power supply",
			ExtractedText: "ZX900Q wall mounted backup power supply specifications",
			Language:      "en",
		},
		{
			NormalizedURL: "https://archive.example/maritime-history",
			Title:         "Maritime history archive",
			ExtractedText: "wall mounted backup power supply historical notes",
			Language:      "en",
		},
	}
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	for _, document := range documents {
		if err := index.Index(t.Context(), document); err != nil {
			t.Fatalf("Index(%s): %v", document.NormalizedURL, err)
		}
	}

	response, err := NewSearcher(index).Search(t.Context(), searchcore.Request{
		Query: "ZX900Q wall mounted backup power supply",
		Terms: []string{"ZX900Q", "wall", "mounted", "backup", "power", "supply"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].URL != documents[0].NormalizedURL {
		t.Fatalf("results = %#v", response.Results)
	}
}
