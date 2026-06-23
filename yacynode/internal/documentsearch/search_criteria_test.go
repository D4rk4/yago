package documentsearch

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func TestSiteHashFromRequestHash(t *testing.T) {
	criteria, err := searchCriteriaFromRequest(yacyproto.SearchRequest{SiteHash: "ABCDEF"})
	if err != nil {
		t.Fatalf("searchCriteriaFromRequest: %v", err)
	}
	if criteria.siteHash != "ABCDEF" {
		t.Fatalf("siteHash = %q, want ABCDEF", criteria.siteHash)
	}
}

func TestSiteHashFromOperatorBeforeStructuredHost(t *testing.T) {
	criteria, err := searchCriteriaFromRequest(yacyproto.SearchRequest{
		Modifier: "site:example.com",
		SiteHost: "ignored.example",
	})
	if err != nil {
		t.Fatalf("searchCriteriaFromRequest: %v", err)
	}

	hash, err := yacymodel.HashURLHost("example.com")
	if err != nil {
		t.Fatalf("HashURLHost: %v", err)
	}
	want, err := hash.HostHash()
	if err != nil {
		t.Fatalf("HostHash: %v", err)
	}
	if criteria.siteHash != want {
		t.Fatalf("siteHash = %q, want %q", criteria.siteHash, want)
	}
}

func TestSiteHashFromStructuredHostFallback(t *testing.T) {
	criteria, err := searchCriteriaFromRequest(yacyproto.SearchRequest{SiteHost: "example.com"})
	if err != nil {
		t.Fatalf("searchCriteriaFromRequest: %v", err)
	}

	hash, err := yacymodel.HashURLHost("example.com")
	if err != nil {
		t.Fatalf("HashURLHost: %v", err)
	}
	want, err := hash.HostHash()
	if err != nil {
		t.Fatalf("HostHash: %v", err)
	}
	if criteria.siteHash != want {
		t.Fatalf("siteHash = %q, want %q", criteria.siteHash, want)
	}
}

func TestLanguageFromOperatorBeforeStructured(t *testing.T) {
	criteria, err := searchCriteriaFromRequest(yacyproto.SearchRequest{
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
	criteria, err := searchCriteriaFromRequest(yacyproto.SearchRequest{Language: "en"})
	if err != nil {
		t.Fatalf("searchCriteriaFromRequest: %v", err)
	}
	if criteria.language != "" {
		t.Fatalf("language = %q, want empty", criteria.language)
	}
}

func TestSearchReportsRequestedTermsAlongsideWantedTerms(t *testing.T) {
	word, related := hashFor("w1"), hashFor("w2")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word:    {postingEntry(word, "u1", 0, 1), postingEntry(word, "u2", 0, 1)},
		related: {postingEntry(related, "u2", 0, 1), postingEntry(related, "u3", 0, 1)},
	}}
	s := searcher{
		index:          index,
		documents:      fakeDirectory{rows: urlRows("u1", "u2")},
		matchesPerTerm: 100,
	}

	result, err := s.search(context.Background(), searchCriteria{
		terms:     []yacymodel.Hash{word},
		reporting: matchReporting{mode: reportRequestedTerms, terms: []yacymodel.Hash{related}},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got := result.documentsMatchingEachReportedTerm[related]; got != "{AAAAAA:u2AAAAu3AAAA}" {
		t.Fatalf("documentsMatchingEachReportedTerm[related] = %q", got)
	}
}
