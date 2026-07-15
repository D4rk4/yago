package searchindex

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestMinimumTermsRequireMixedAlphanumericIdentifier(t *testing.T) {
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
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{documents: documents})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query:              "ZX900Q wall mounted backup power supply",
		Terms:              []string{"ZX900Q", "wall", "mounted", "backup", "power", "supply"},
		MinimumTermMatches: 4,
		Relaxed:            true,
		MaxResults:         10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].URL != documents[0].NormalizedURL {
		t.Fatalf("results = %#v", result.Results)
	}
}

func TestRelaxedEvidenceRequiresExactIdentifierInPassage(t *testing.T) {
	req := SearchRequest{
		Query:   "ZX900Q wall mounted backup power supply",
		Terms:   []string{"ZX900Q", "wall", "mounted", "backup", "power", "supply"},
		Relaxed: true,
	}
	candidate := SearchResult{RelaxedRank: 1}
	if relaxedCandidateFound(
		t,
		req,
		"wall mounted backup power supply historical notes",
		candidate,
	) {
		t.Fatal("missing identifier admitted a relaxed candidate")
	}
	if relaxedCandidateFound(
		t,
		req,
		"ZX900Q "+strings.Repeat("background ", 80)+"wall mounted backup power supply",
		candidate,
	) {
		t.Fatal("distant identifier admitted an unrelated passage")
	}
	if !relaxedCandidateFound(
		t,
		req,
		"ZX900Q wall mounted backup power supply specifications",
		candidate,
	) {
		t.Fatal("exact identifier passage was rejected")
	}
}
