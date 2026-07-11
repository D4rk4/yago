package searchindex

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestSafeSearchFiltersExplicitDocumentsBeforeResultMapping(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	for _, doc := range []documentstore.Document{
		{
			NormalizedURL: "https://example.org/general",
			Title:         "alpha general",
			ContentSafety: documentstore.ContentSafetyEvidence{
				Rating: documentstore.SafetyGeneral, ExplicitProbability: 0.1, Confidence: 0.8,
			},
		},
		{
			NormalizedURL: "https://example.org/explicit",
			Title:         "alpha explicit",
			ContentSafety: documentstore.ContentSafetyEvidence{
				Rating: documentstore.SafetyExplicit, ExplicitProbability: 0.9, Confidence: 0.8,
			},
		},
		{NormalizedURL: "https://example.org/unknown", Title: "alpha unknown"},
	} {
		if err := index.Index(t.Context(), doc); err != nil {
			t.Fatalf("Index: %v", err)
		}
	}

	strict, err := index.Search(t.Context(), SearchRequest{
		Query: "alpha", MaxResults: 10, SafeSearch: true, ContentDomain: "text",
	})
	if err != nil {
		t.Fatalf("strict Search: %v", err)
	}
	if strict.Total != 2 || len(strict.Results) != 2 {
		t.Fatalf("strict results = %#v", strict)
	}
	var general SearchResult
	for _, result := range strict.Results {
		if result.URL == "https://example.org/explicit" {
			t.Fatalf("explicit result leaked: %#v", strict.Results)
		}
		if result.URL == "https://example.org/general" {
			general = result
		}
	}
	if general.SafetyRating != documentstore.SafetyGeneral ||
		general.ExplicitProbability != 0.1 || general.SafetyConfidence != 0.8 {
		t.Fatalf("general evidence = %#v", general)
	}

	unfiltered, err := index.Search(t.Context(), SearchRequest{Query: "alpha", MaxResults: 10})
	if err != nil || unfiltered.Total != 3 {
		t.Fatalf("unfiltered results = %#v/%v", unfiltered, err)
	}
}

func TestSafeSearchSuppressesUnknownImages(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	for _, doc := range []documentstore.Document{
		{
			NormalizedURL: "https://example.org/general.png",
			Title:         "alpha image",
			ContentType:   "image/png",
			Images:        []documentstore.ImageMetadata{{URL: "https://example.org/general.png"}},
			ContentSafety: documentstore.ContentSafetyEvidence{Rating: documentstore.SafetyGeneral},
		},
		{
			NormalizedURL: "https://example.org/unknown.png",
			Title:         "alpha image",
			ContentType:   "image/png",
			Images:        []documentstore.ImageMetadata{{URL: "https://example.org/unknown.png"}},
		},
	} {
		if err := index.Index(t.Context(), doc); err != nil {
			t.Fatalf("Index: %v", err)
		}
	}
	results, err := index.Search(t.Context(), SearchRequest{
		Query: "alpha", MaxResults: 10, SafeSearch: true, ContentDomain: "IMAGE",
	})
	if err != nil || results.Total != 1 || len(results.Results) != 1 ||
		results.Results[0].URL != "https://example.org/general.png" {
		t.Fatalf("image results = %#v/%v", results, err)
	}
	if !allowsSafeDocument(documentstore.Document{}, "text") {
		t.Fatal("unknown text document should remain eligible")
	}
}
