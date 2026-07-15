package websearch

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestCoveredDistinctTermsRequiresIndependentTokenWitnesses(t *testing.T) {
	result := searchcore.Result{
		Title: "Checkpoint reference",
		URL:   "https://example.org/reference",
	}
	if got := coveredDistinctTerms(result, []string{"check", "point"}); got != 1 {
		t.Fatalf("covered terms = %d, want 1", got)
	}
	if resultCoversTerms(result, []string{"check", "point"}, 2) {
		t.Fatal("one checkpoint token satisfied two requirements")
	}
}

func TestCoveredDistinctTermsRejectsShortInteriorSubstrings(t *testing.T) {
	result := searchcore.Result{
		Title:   "Capital planning",
		Snippet: "Rapid delivery",
		URL:     "https://capital.example/rapid",
	}
	if got := coveredDistinctTerms(result, []string{"api"}); got != 0 {
		t.Fatalf("covered terms = %d, want 0", got)
	}
}

func TestCoveredDistinctTermsHandlesEmptyRequirementsAndRawURL(t *testing.T) {
	result := searchcore.Result{URL: "https://example.org/%zz/api"}
	if got := coveredDistinctTerms(result, nil); got != 0 {
		t.Fatalf("empty covered terms = %d, want 0", got)
	}
	got := distinctVerificationTerms([]string{" ", "API", "api"})
	if len(got) != 1 || got[0] != "api" {
		t.Fatalf("distinct terms = %#v", got)
	}
	if got := coveredDistinctTerms(result, []string{"api"}); got != 1 {
		t.Fatalf("raw URL covered terms = %d, want 1", got)
	}
}

func TestCoveredDistinctTermsKeepsTokenMorphologyAndDecodedURL(t *testing.T) {
	result := searchcore.Result{
		Title:   "Путеводитель по Черногории",
		Snippet: "Stable API reference",
		URL:     "https://example.org/%D0%BC%D0%BE%D1%80%D0%B5",
	}
	terms := []string{"черногория", "api", "море"}
	if got := coveredDistinctTerms(result, terms); got != len(terms) {
		t.Fatalf("covered terms = %d, want %d", got, len(terms))
	}
}

func TestCoveredDistinctTermsKeepsUnsegmentedOccurrences(t *testing.T) {
	result := searchcore.Result{Title: "東京大学案内"}
	if got := coveredDistinctTerms(result, []string{"東京", "大学"}); got != 2 {
		t.Fatalf("covered terms = %d, want 2", got)
	}
}

func TestCoveredDistinctTermsBoundsProviderFields(t *testing.T) {
	result := searchcore.Result{
		Title:   strings.Repeat("x", maximumVerificationTitleRunes+32) + " needle",
		Snippet: strings.Repeat("y ", maximumVerificationTokens+32) + "needle",
		URL:     "https://example.org/needle",
	}
	if got := coveredDistinctTerms(result, []string{"needle"}); got != 1 {
		t.Fatalf("covered terms = %d, want 1", got)
	}
}

func TestMaximumDistinctWitnessesReassignsGreedyChoice(t *testing.T) {
	edges := [][]int{{0, 1}, {0}}
	if got := maximumDistinctWitnesses(edges, 2); got != 2 {
		t.Fatalf("matched witnesses = %d, want 2", got)
	}
}
