package documentsearch

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestSiteHashFromRequestHash(t *testing.T) {
	criteria, err := searchCriteriaFromRequest(yagoproto.SearchRequest{SiteHash: "ABCDEF"})
	if err != nil {
		t.Fatalf("searchCriteriaFromRequest: %v", err)
	}
	requireSiteHashes(t, criteria.siteHashes, "ABCDEF")
}

func TestSiteHashFromOperatorBeforeStructuredHost(t *testing.T) {
	criteria, err := searchCriteriaFromRequest(yagoproto.SearchRequest{
		Modifier: "site:example.com",
		SiteHost: "ignored.example",
		SiteHash: "ABCDEF",
	})
	if err != nil {
		t.Fatalf("searchCriteriaFromRequest: %v", err)
	}

	requireSiteHashes(
		t,
		criteria.siteHashes,
		siteHostHash(t, "example.com"),
		siteHostHash(t, "www.example.com"),
	)
}

func TestSiteHashFromStructuredHostFallback(t *testing.T) {
	criteria, err := searchCriteriaFromRequest(yagoproto.SearchRequest{SiteHost: "example.com"})
	if err != nil {
		t.Fatalf("searchCriteriaFromRequest: %v", err)
	}

	requireSiteHashes(
		t,
		criteria.siteHashes,
		siteHostHash(t, "example.com"),
		siteHostHash(t, "www.example.com"),
	)
}

func siteHostHash(t *testing.T, host string) string {
	t.Helper()
	hash, err := yagomodel.HashURLHost(host)
	if err != nil {
		t.Fatalf("HashURLHost(%q): %v", host, err)
	}
	hostHash, err := hash.HostHash()
	if err != nil {
		t.Fatalf("HostHash(%q): %v", host, err)
	}

	return hostHash
}

func requireSiteHashes(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("siteHashes = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("siteHashes = %v, want %v", got, want)
		}
	}
}

func TestLanguageFromOperatorBeforeStructured(t *testing.T) {
	criteria, err := searchCriteriaFromRequest(yagoproto.SearchRequest{
		Modifier: "/language/de",
		Language: "en",
	})
	if err != nil {
		t.Fatalf("searchCriteriaFromRequest: %v", err)
	}
	if criteria.language != "de" {
		t.Fatalf("language = %q, want de", criteria.language)
	}
}

func TestStructuredLanguageDoesNotFilter(t *testing.T) {
	criteria, err := searchCriteriaFromRequest(yagoproto.SearchRequest{Language: "en"})
	if err != nil {
		t.Fatalf("searchCriteriaFromRequest: %v", err)
	}
	if criteria.language != "" {
		t.Fatalf("language = %q, want empty", criteria.language)
	}
}

func TestSearchReportsRequestedTermsAlongsideWantedTerms(t *testing.T) {
	word, related := hashFor("w1"), hashFor("w2")
	index := fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
		word:    {postingEntry(word, "u1", 0, 1), postingEntry(word, "u2", 0, 1)},
		related: {postingEntry(related, "u2", 0, 1), postingEntry(related, "u3", 0, 1)},
	}}
	s := searcher{
		index:          index,
		documents:      fakeDirectory{rows: urlRows("u1", "u2")},
		matchesPerTerm: 100,
	}

	result, err := s.search(context.Background(), searchCriteria{
		terms:     []yagomodel.Hash{word},
		reporting: matchReporting{mode: reportRequestedTerms, terms: []yagomodel.Hash{related}},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got := result.documentsMatchingEachReportedTerm[related]; got != "{AAAAAA:u2AAAAu3AAAA}" {
		t.Fatalf("documentsMatchingEachReportedTerm[related] = %q", got)
	}
}
