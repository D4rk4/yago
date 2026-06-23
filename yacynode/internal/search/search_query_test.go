package search

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestJoinSiteHashFromHash(t *testing.T) {
	q := searchQuery{searchFilters: searchFilters{SiteHash: "ABCDEF"}}
	got, err := q.joinSiteHash()
	if err != nil {
		t.Fatalf("joinSiteHash: %v", err)
	}
	if got != "ABCDEF" {
		t.Fatalf("joinSiteHash = %q, want ABCDEF", got)
	}
}

func TestJoinSiteHashFromModifier(t *testing.T) {
	q := searchQuery{searchFilters: searchFilters{Modifier: "site:example.com"}}
	got, err := q.joinSiteHash()
	if err != nil {
		t.Fatalf("joinSiteHash: %v", err)
	}

	hash, err := yacymodel.HashURLHost("example.com")
	if err != nil {
		t.Fatalf("HashURLHost: %v", err)
	}
	want, err := hash.HostHash()
	if err != nil {
		t.Fatalf("HostHash: %v", err)
	}
	if got != want {
		t.Fatalf("joinSiteHash = %q, want %q", got, want)
	}
}

func TestJoinSiteHashEmpty(t *testing.T) {
	got, err := searchQuery{}.joinSiteHash()
	if err != nil {
		t.Fatalf("joinSiteHash: %v", err)
	}
	if got != "" {
		t.Fatalf("joinSiteHash = %q, want empty", got)
	}
}

func TestSearchExplicitAbstractWithQuery(t *testing.T) {
	word, related := hashFor("w1"), hashFor("w2")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word:    {postingEntry(word, "u1", 0, 1), postingEntry(word, "u2", 0, 1)},
		related: {postingEntry(related, "u2", 0, 1), postingEntry(related, "u3", 0, 1)},
	}}
	s := searcher{
		index:           index,
		urls:            fakeDirectory{rows: urlRows("u1", "u2")},
		postingsPerWord: 100,
	}

	result, err := s.Search(context.Background(), searchQuery{
		Words:     []yacymodel.Hash{word},
		Abstracts: abstractSpec{Mode: abstractExplicit, Words: []yacymodel.Hash{related}},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := result.Abstracts[related]; got != "{AAAAAA:u2AAAAu3AAAA}" {
		t.Fatalf("Abstracts[related] = %q", got)
	}
}
