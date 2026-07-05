package yacysearch

import (
	"strings"
	"testing"
)

func TestMergeSuggestionsPrefersIndexThenFillsFromRecent(t *testing.T) {
	got := mergeSuggestions(
		10,
		[]string{"linux kernel", "Linux distros"},
		[]string{"LINUX KERNEL", "linux laptops"}, // first is a dup, second is fresh
	)

	want := []string{"linux kernel", "Linux distros", "linux laptops"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("merged = %v, want index titles first then unique recent queries", got)
	}
}

func TestMergeSuggestionsCapsAtLimit(t *testing.T) {
	got := mergeSuggestions(2, []string{"a", "b", "c"}, []string{"d"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("merged = %v, want the first two only", got)
	}
}

func TestMergeSuggestionsDefaultsNonPositiveLimit(t *testing.T) {
	group := make([]string, 0, publicSuggestionLimit+3)
	for i := 0; i < publicSuggestionLimit+3; i++ {
		group = append(group, strings.Repeat("q", i+1))
	}
	got := mergeSuggestions(0, group)
	if len(got) != publicSuggestionLimit {
		t.Fatalf("merged = %d, want the default limit %d", len(got), publicSuggestionLimit)
	}
}

func TestMergeSuggestionsAlwaysReturnsNonNil(t *testing.T) {
	if got := mergeSuggestions(5); got == nil {
		t.Fatal("merged = nil, want a non-nil empty slice so the endpoint encodes []")
	}
}
