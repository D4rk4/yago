package pageindex_test

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
	"github.com/D4rk4/yago/yagomodel"
)

// TestBuildPostingsSkipsStopwords pins SEARCH-39: stopword tokens produce no
// RWI postings, while content tokens keep their position in the original
// token stream (stopwords included) and the shared word counts still reflect
// the full stream.
func TestBuildPostingsSkipsStopwords(t *testing.T) {
	page := pageparse.ParsedPage{
		URL:      "http://example.com/",
		Title:    "The Outback",
		Language: "en",
		Text:     "the kangaroo and the outback",
	}
	postings := buildPostings(t, page)

	byHash := make(map[yagomodel.Hash]yagomodel.RWIPosting, len(postings))
	for _, entry := range postings {
		byHash[entry.WordHash] = entry
	}
	for _, stopword := range []string{"the", "and"} {
		if _, ok := byHash[yagomodel.WordHash(stopword)]; ok {
			t.Errorf("stopword %q must not produce a posting", stopword)
		}
	}
	if len(postings) != 2 {
		t.Fatalf("postings = %d, want 2 (kangaroo, outback)", len(postings))
	}
	assertTokenPosition(t, byHash, "kangaroo", 1)
	assertTokenPosition(t, byHash, "outback", 4)
	kangaroo := byHash[yagomodel.WordHash("kangaroo")]
	if got := kangaroo.Properties[yagomodel.ColTextWordCount]; got != yagomodel.FormatRWICardinal(
		5,
	) {
		t.Errorf("text word count = %q, want the full stream of 5", got)
	}
	if got := kangaroo.Properties[yagomodel.ColTitleWordCount]; got != yagomodel.FormatRWICardinal(
		2,
	) {
		t.Errorf("title word count = %q, want 2", got)
	}
}

func assertTokenPosition(
	t *testing.T,
	byHash map[yagomodel.Hash]yagomodel.RWIPosting,
	token string,
	position uint64,
) {
	t.Helper()
	entry, ok := byHash[yagomodel.WordHash(token)]
	if !ok {
		t.Fatalf("no posting for %q", token)
	}
	if got := entry.Properties[yagomodel.ColTextPosition]; got != yagomodel.FormatRWICardinal(
		position,
	) {
		t.Errorf("%q position = %q, want %q", token, got, yagomodel.FormatRWICardinal(position))
	}
}
