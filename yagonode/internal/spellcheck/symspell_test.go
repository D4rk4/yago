package spellcheck

import (
	"strings"
	"testing"
)

func TestCorrectorSuggest(t *testing.T) {
	corrector := New(map[string]int{
		"golang":   10,
		"tutorial": 8,
		"gulang":   1,
		"":         5, // dropped: blank
		"zero":     0, // dropped: non-positive frequency
	})

	// A one-edit typo corrects to the far more frequent word.
	if got, ok := corrector.Suggest("golnag"); !ok || got != "golang" {
		t.Fatalf("Suggest(golnag) = %q %v, want golang", got, ok)
	}
	// A word already in the dictionary is left alone.
	if got, ok := corrector.Suggest("golang"); ok || got != "golang" {
		t.Fatalf("Suggest(golang) = %q %v, want unchanged", got, ok)
	}
	// Frequency breaks ties between two equally-close candidates.
	if got, _ := corrector.Suggest("golung"); got != "golang" {
		t.Fatalf("Suggest(golung) = %q, want the frequent golang", got)
	}
	// Too short to risk a two-edit correction.
	if got, ok := corrector.Suggest("go"); ok || got != "go" {
		t.Fatalf("Suggest(go) = %q %v, want unchanged", got, ok)
	}
	// Beyond the edit budget, no suggestion.
	if got, ok := corrector.Suggest("xyzzyplugh"); ok || got != "xyzzyplugh" {
		t.Fatalf("Suggest(xyzzyplugh) = %q %v, want unchanged", got, ok)
	}
}

func TestCorrectorTieBreaking(t *testing.T) {
	// Three candidates at equal edit distance from "aaxx": frequency decides
	// first (aadd loses), then the term itself breaks the aabb/aacc tie.
	corrector := New(map[string]int{"aabb": 5, "aacc": 5, "aadd": 3})
	if got, ok := corrector.Suggest("aaxx"); !ok || got != "aabb" {
		t.Fatalf("Suggest(aaxx) = %q %v, want aabb", got, ok)
	}
}

func TestCorrectorDropsShortDictionaryTerms(t *testing.T) {
	corrector := New(map[string]int{"ab": 3, "golang": 4})
	if _, found := corrector.frequency["ab"]; found {
		t.Fatal("short dictionary term was retained")
	}
	if got, ok := corrector.Suggest("golnag"); !ok || got != "golang" {
		t.Fatalf("Suggest(golnag) = %q %v, want golang", got, ok)
	}
}

func TestCorrectorRejectsOversizedTerms(t *testing.T) {
	oversized := strings.Repeat("a", defaultMaxTermLen+1)
	corrector := New(map[string]int{oversized: 10, "golang": 5})
	if len(corrector.frequency) != 1 {
		t.Fatalf("dictionary = %#v", corrector.frequency)
	}
	if got, ok := corrector.Suggest(oversized); ok || got != oversized {
		t.Fatalf("oversized suggestion = %q/%v", got, ok)
	}
}

func TestCorrectorEmptyAndNil(t *testing.T) {
	var nilCorrector *Corrector
	if got, ok := nilCorrector.Suggest("golnag"); ok || got != "golnag" {
		t.Fatalf("nil corrector suggested: %q %v", got, ok)
	}
	empty := New(nil)
	if got, ok := empty.Suggest("golnag"); ok || got != "golnag" {
		t.Fatalf("empty corrector suggested: %q %v", got, ok)
	}
	if got := empty.CorrectQuery([]string{"golnag"}); got != "" {
		t.Fatalf("empty corrector corrected query: %q", got)
	}
}

func TestCorrectQuery(t *testing.T) {
	corrector := New(map[string]int{"golang": 10, "tutorial": 8})

	// One term fixed, one already correct → rebuilt query.
	if got := corrector.CorrectQuery([]string{"golnag", "tutorial"}); got != "golang tutorial" {
		t.Fatalf("CorrectQuery = %q, want %q", got, "golang tutorial")
	}
	// Nothing wrong → empty, so no "did you mean" is offered.
	if got := corrector.CorrectQuery([]string{"golang", "tutorial"}); got != "" {
		t.Fatalf("CorrectQuery(correct) = %q, want empty", got)
	}
	// An uncorrectable term is preserved beside a corrected one.
	if got := corrector.CorrectQuery([]string{"golnag", "zz"}); got != "golang zz" {
		t.Fatalf("CorrectQuery = %q, want %q", got, "golang zz")
	}
}

func TestTermFrequencies(t *testing.T) {
	freq := map[string]int{}
	oversized := strings.Repeat("a", defaultMaxTermLen+1)
	TermFrequencies(freq, "Montenegro, montenegro! A tiny — go — країна "+oversized)
	if freq["montenegro"] != 2 {
		t.Fatalf("montenegro count = %d, want 2", freq["montenegro"])
	}
	if _, ok := freq["go"]; ok {
		t.Fatal("short token should be skipped")
	}
	if freq["країна"] != 1 {
		t.Fatalf("unicode token count = %d, want 1", freq["країна"])
	}
	if _, found := freq[oversized]; found {
		t.Fatal("oversized token should be skipped")
	}
}

func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"golnag", "golang", 2},
		{"kitten", "sitting", 3},
	}
	for _, tc := range cases {
		if got := editDistance(tc.a, tc.b); got != tc.want {
			t.Fatalf("editDistance(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestDeleteVariantsStopsAtOneRune(t *testing.T) {
	variants := deleteVariants("a", 2)
	if len(variants) != 1 || !variants["a"] {
		t.Fatalf("variants = %#v", variants)
	}
}

func TestHolder(t *testing.T) {
	holder := NewHolder()
	if _, ok := holder.Current().Suggest("golnag"); ok {
		t.Fatal("fresh holder should correct nothing")
	}
	holder.Store(New(map[string]int{"golang": 5}))
	if got, ok := holder.Current().Suggest("golnag"); !ok || got != "golang" {
		t.Fatalf("stored corrector = %q %v", got, ok)
	}
	// A nil store resets to an empty corrector rather than publishing nil.
	holder.Store(nil)
	if _, ok := holder.Current().Suggest("golnag"); ok {
		t.Fatal("nil store should reset to empty")
	}
}
