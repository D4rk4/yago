package yacysearch

import (
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func titledSearch(titles ...string) *fakeSearch {
	results := make([]searchcore.Result, 0, len(titles))
	for _, title := range titles {
		results = append(results, searchcore.Result{Title: title})
	}

	return &fakeSearch{response: searchcore.Response{Results: results}}
}

func TestIndexSuggesterReturnsDedupedLocalTitles(t *testing.T) {
	search := titledSearch(
		"Linux kernel newbies",
		"linux KERNEL newbies", // case-insensitive duplicate of the first
		"Linux kernel scheduling internals",
	)
	got := indexSuggester{search: search}.Suggest(t.Context(), "linux ker", publicSuggestionLimit)

	want := []string{"Linux kernel newbies", "Linux kernel scheduling internals"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("suggestions = %v, want %v", got, want)
	}
	if search.got.Source != searchcore.SourceLocal {
		t.Fatalf("search source = %q, want local-only", search.got.Source)
	}
	if search.got.Query != "linux ker" || len(search.got.Terms) == 0 {
		t.Fatalf(
			"request query=%q terms=%v, want the prefix parsed into terms",
			search.got.Query,
			search.got.Terms,
		)
	}
}

func TestIndexSuggesterSkipsBlankAndEchoTitles(t *testing.T) {
	search := titledSearch("   ", "linux", "Linux distributions compared")
	got := indexSuggester{search: search}.Suggest(t.Context(), "Linux", publicSuggestionLimit)

	if len(got) != 1 || got[0] != "Linux distributions compared" {
		t.Fatalf("suggestions = %v, want only the non-blank, non-echo title", got)
	}
}

func TestIndexSuggesterCapsAtLimit(t *testing.T) {
	search := titledSearch("alpha one", "beta two", "gamma three", "delta four")
	got := indexSuggester{search: search}.Suggest(t.Context(), "a", 2)

	if len(got) != 2 {
		t.Fatalf("suggestions = %v, want exactly 2 (the limit)", got)
	}
}

func TestIndexSuggesterTruncatesLongTitle(t *testing.T) {
	long := strings.Repeat("x", suggestionMaxRunes+40)
	got := indexSuggester{
		search: titledSearch(long),
	}.Suggest(
		t.Context(),
		"x",
		publicSuggestionLimit,
	)

	if len(got) != 1 || len([]rune(got[0])) != suggestionMaxRunes {
		t.Fatalf("suggestion runes = %d, want %d", len([]rune(got[0])), suggestionMaxRunes)
	}
}

func TestIndexSuggesterDefaultsLimitWhenNonPositive(t *testing.T) {
	titles := make([]string, 0, publicSuggestionLimit+5)
	for i := 0; i < publicSuggestionLimit+5; i++ {
		titles = append(titles, "distinct suggestion "+strings.Repeat("z", i+1))
	}
	got := indexSuggester{search: titledSearch(titles...)}.Suggest(t.Context(), "distinct", 0)

	if len(got) != publicSuggestionLimit {
		t.Fatalf("suggestions = %d, want the default limit %d", len(got), publicSuggestionLimit)
	}
}

func TestIndexSuggesterReturnsNilForEmptyPrefixOrNoSearcher(t *testing.T) {
	if got := (indexSuggester{search: titledSearch("x")}).Suggest(
		t.Context(),
		"  ",
		publicSuggestionLimit,
	); got != nil {
		t.Fatalf("blank prefix suggestions = %v, want nil", got)
	}
	if got := (indexSuggester{}).Suggest(t.Context(), "linux", publicSuggestionLimit); got != nil {
		t.Fatalf("nil-searcher suggestions = %v, want nil", got)
	}
}

func TestIndexSuggesterReturnsNilOnSearchError(t *testing.T) {
	search := &fakeSearch{err: errors.New("index unavailable")}
	if got := (indexSuggester{search: search}).Suggest(
		t.Context(),
		"linux",
		publicSuggestionLimit,
	); got != nil {
		t.Fatalf("suggestions on error = %v, want nil", got)
	}
}
