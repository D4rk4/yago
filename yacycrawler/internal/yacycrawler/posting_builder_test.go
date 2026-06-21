package yacycrawler_test

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/yacycrawler"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestBuildPostingsUsesYaCyWordHash(t *testing.T) {
	page := yacycrawler.ParsedPage{
		URL:      "http://example.com/",
		Title:    "Kangaroo facts",
		Language: "en",
		Text:     "Kangaroo kangaroo hops across the outback",
		Links:    nil,
	}
	postings := yacycrawler.BuildPostings(page)

	byHash := make(map[yacymodel.Hash]yacymodel.RWIPosting, len(postings))
	for _, entry := range postings {
		byHash[entry.WordHash] = entry
	}

	want := yacymodel.WordHash("kangaroo")
	entry, ok := byHash[want]
	if !ok {
		t.Fatalf("no posting for word hash %q", want)
	}
	if got := entry.Properties[yacymodel.ColHitCount]; got == "" {
		t.Errorf("missing hit count on kangaroo posting")
	}
}

func TestBuildPostingsAreAcceptableRWILines(t *testing.T) {
	page := yacycrawler.ParsedPage{
		URL:      "http://example.com/path?q=a&b=c",
		Title:    "Title",
		Language: "en",
		Text:     "indexable words here",
		Links:    []string{"http://example.com/local", "http://other.com/x"},
	}
	postings := yacycrawler.BuildPostings(page)
	if len(postings) == 0 {
		t.Fatal("expected postings")
	}
	for _, entry := range postings {
		line := entry.String()
		if !yacymodel.AcceptableRWILine(line) {
			t.Errorf("posting not acceptable: %q", line)
		}
		parsed, err := yacymodel.ParseRWIPosting(line)
		if err != nil {
			t.Errorf("ParseRWIPosting(%q): %v", line, err)
			continue
		}
		if parsed.WordHash != entry.WordHash {
			t.Errorf("round trip word hash %q != %q", parsed.WordHash, entry.WordHash)
		}
		hits, err := parsed.Cardinal(yacymodel.ColHitCount)
		if err != nil {
			t.Errorf("hit count cardinal: %v", err)
		}
		if hits == 0 {
			t.Errorf("hit count must survive parser normalization: %q", line)
		}
	}
}
