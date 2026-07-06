package searchindex

import (
	"testing"

	"github.com/blevesearch/bleve/v2/mapping"
)

func TestStemWord(t *testing.T) {
	cases := map[string]string{
		"":           "",         // empty folds to empty
		"ЧерноГория": "черногор", // lowercased, stemmed
		"черногории": "черногор", // an inflected form shares the stem
		"running":    "run",      // English stemmer
		"the":        "the",      // stop-filtered to zero tokens: word unchanged
		"שלום":       "שלום",     // script with no stemmer: standard analyzer path
	}
	for word, want := range cases {
		if got := StemWord(word); got != want {
			t.Fatalf("StemWord(%q) = %q, want %q", word, got, want)
		}
	}
	// Surrounding whitespace is trimmed before stemming.
	if got := StemWord("  Черногория "); got != "черногор" {
		t.Fatalf("StemWord with padding = %q, want черногор", got)
	}
}

func TestStemWordNilMapping(t *testing.T) {
	original := loadStemmingMapping
	t.Cleanup(func() { loadStemmingMapping = original })

	// A nil mapping (build failure) folds the word rather than stemming it.
	loadStemmingMapping = func() *mapping.IndexMappingImpl { return nil }
	if got := StemWord("черногория"); got != "черногория" {
		t.Fatalf("nil mapping: StemWord = %q", got)
	}
}
