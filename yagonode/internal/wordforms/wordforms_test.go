package wordforms

import (
	"slices"
	"strings"
	"testing"
)

// prefixStem is a deterministic test stemmer: it groups words by their first
// four runes, standing in for a real language analyzer.
func prefixStem(word string) string {
	runes := []rune(word)
	if len(runes) <= 4 {
		return word
	}

	return string(runes[:4])
}

func TestExpanderGroupsSurfaceFormsByStem(t *testing.T) {
	vocabulary := map[string]int{
		"черногория": 5,
		"черногории": 3,
		"черногорию": 2,
		"чернила":    9, // different stem (черн != черн...): shares first 4 "черн"
		"unrelated":  4,
	}
	expander := New(vocabulary, prefixStem)

	got := expander.Variants("черногория")
	if got[0] != "черногория" {
		t.Fatalf("original not first: %v", got)
	}
	for _, want := range []string{"черногории", "черногорию"} {
		if !slices.Contains(got, want) {
			t.Fatalf("inflected form %q missing from %v", want, got)
		}
	}
	if slices.Contains(got, "unrelated") {
		t.Fatalf("cross-stem form leaked: %v", got)
	}
	if len(got) > MaximumVariants {
		t.Fatalf("over the cap: %v", got)
	}
}

func TestExpanderSkipsAndOrdersTies(t *testing.T) {
	stem := func(string) string { return "monten" }
	vocabulary := map[string]int{
		"montenegro": 4,
		"montenegri": 4, // equal frequency: ordered by the form for determinism
		"abc":        9, // shorter than the minimum: skipped
		"montenulls": 0, // non-positive count: skipped
	}
	expander := New(vocabulary, stem)
	got := expander.Variants("montenegro")
	if !slices.Contains(got, "montenegri") {
		t.Fatalf("equal-frequency form dropped: %v", got)
	}
	if slices.Contains(got, "abc") || slices.Contains(got, "montenulls") {
		t.Fatalf("skipped term leaked: %v", got)
	}
}

func TestExpanderFrequencyOrderAndCap(t *testing.T) {
	// One stem with more forms than the per-stem cap keeps the most frequent.
	vocabulary := map[string]int{}
	stem := func(string) string { return "stem" }
	for i := range maxFormsPerStem + 3 {
		vocabulary["formaaaa"+strings.Repeat("x", i)] = i + 1
	}
	expander := New(vocabulary, stem)
	got := expander.Variants("formaaaquery")
	if len(got) > MaximumVariants {
		t.Fatalf("variants exceed the cap: %v", got)
	}
}

func TestExpanderNoExpansion(t *testing.T) {
	expander := New(map[string]int{"черногория": 5, "черногории": 3}, prefixStem)

	// Short query word: just itself.
	if got := expander.Variants("abc"); len(got) != 1 || got[0] != "abc" {
		t.Fatalf("short word expanded: %v", got)
	}
	// A stem with no other observed forms returns just the word.
	if got := expander.Variants("solitary"); len(got) != 1 || got[0] != "solitary" {
		t.Fatalf("lone stem expanded: %v", got)
	}
}

func TestExpanderNilAndEmpty(t *testing.T) {
	var nilExpander *Expander
	if got := nilExpander.Variants("черногория"); len(got) != 1 || got[0] != "черногория" {
		t.Fatalf("nil expander = %v", got)
	}
	// No stemmer injected: expands nothing.
	noStem := New(map[string]int{"черногория": 5}, nil)
	if got := noStem.Variants("черногория"); len(got) != 1 {
		t.Fatalf("stemless expander expanded: %v", got)
	}
}

func TestHolder(t *testing.T) {
	holder := NewHolder()
	if got := holder.Current().Variants("черногория"); len(got) != 1 {
		t.Fatalf("fresh holder expanded: %v", got)
	}
	holder.Store(New(map[string]int{"черногория": 5, "черногории": 3}, prefixStem))
	if got := holder.Current().Variants("черногория"); len(got) < 2 {
		t.Fatalf("stored expander did not expand: %v", got)
	}
	holder.Store(nil)
	if got := holder.Current().Variants("черногория"); len(got) != 1 {
		t.Fatalf("nil store did not reset: %v", got)
	}
}
