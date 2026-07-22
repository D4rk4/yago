package documentsearch

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

func searchIdentity() nodeidentity.Identity {
	return nodeidentity.Identity{Hash: yagomodel.WordHash("self"), NetworkName: "freeworld"}
}

func newEndpoint(
	index fakeScanner,
	documents fakeDirectory,
) func(context.Context, yagoproto.SearchRequest) (yagoproto.SearchResponse, error) {
	return searchEndpoint{
		identity: searchIdentity(),
		searcher: searcher{
			index:          index,
			documents:      documents,
			matchesPerTerm: 100,
		},
	}.Serve
}

func serveSearch(
	t *testing.T,
	endpoint func(context.Context, yagoproto.SearchRequest) (yagoproto.SearchResponse, error),
	req yagoproto.SearchRequest,
) yagoproto.SearchResponse {
	t.Helper()

	resp, err := endpoint(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}

	return resp
}

func TestEndpointJoinsAndAnswers(t *testing.T) {
	word := hashFor("w1")
	index := fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
		word: {postingEntry(word, "u1", 0, 1), postingEntry(word, "u2", 0, 1)},
	}}
	endpoint := newEndpoint(index, fakeDirectory{rows: urlRows("u1", "u2")})

	resp := serveSearch(t, endpoint, yagoproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yagomodel.Hash{word},
		Count:       10,
	})

	if resp.Count != 2 || resp.JoinCount != 2 {
		t.Errorf("Count = %d, JoinCount = %d, want 2/2", resp.Count, resp.JoinCount)
	}
}

func TestEndpointSiteModifierAcceptsBareAndWWWOnly(t *testing.T) {
	word := hashFor("w1")
	bare := mustURLHash(t, "http://example.org/page")
	www := mustURLHash(t, "http://www.example.org/page")
	subdomain := mustURLHash(t, "http://docs.example.org/page")
	index := fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
		word: {
			postingForURLHash(word, bare),
			postingForURLHash(word, www),
			postingForURLHash(word, subdomain),
		},
	}}
	documents := fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
		bare.Hash(): {Properties: map[string]string{
			yagomodel.URLMetaHash: bare.String(),
		}},
		www.Hash(): {Properties: map[string]string{
			yagomodel.URLMetaHash: www.String(),
		}},
		subdomain.Hash(): {Properties: map[string]string{
			yagomodel.URLMetaHash: subdomain.String(),
		}},
	}}
	endpoint := newEndpoint(index, documents)

	for _, host := range []string{"example.org", "www.example.org"} {
		resp := serveSearch(t, endpoint, yagoproto.SearchRequest{
			NetworkName: "freeworld",
			Query:       []yagomodel.Hash{word},
			Count:       10,
			Modifier:    "site:" + host,
			SiteHash:    siteHostHash(t, host),
		})
		if resp.Count != 2 || resp.JoinCount != 2 {
			t.Fatalf("site:%s count/join = %d/%d, want 2/2", host, resp.Count, resp.JoinCount)
		}
		for _, resource := range resp.Resources {
			if resource.Properties[yagomodel.URLMetaHash] == subdomain.String() {
				t.Fatalf("site:%s admitted arbitrary subdomain", host)
			}
		}
	}
}

func mustURLHash(t *testing.T, rawURL string) yagomodel.URLHash {
	t.Helper()
	hash, err := yagomodel.HashURL(rawURL)
	if err != nil {
		t.Fatalf("HashURL(%q): %v", rawURL, err)
	}

	return hash
}

func postingForURLHash(word yagomodel.Hash, location yagomodel.URLHash) yagomodel.RWIPosting {
	return yagomodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yagomodel.ColURLHash:      location.String(),
			yagomodel.ColHitCount:     "1",
			yagomodel.ColTextPosition: "0",
		},
	}
}

func TestEndpointReportsTermWithMostMatches(t *testing.T) {
	word1, word2 := hashFor("w1"), hashFor("w2")
	index := fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
		word1: {postingEntry(word1, "u1", 0, 1), postingEntry(word1, "u2", 0, 1)},
		word2: {postingEntry(word2, "u2", 0, 1)},
	}}
	endpoint := newEndpoint(index, fakeDirectory{rows: urlRows("u1", "u2")})

	resp := serveSearch(t, endpoint, yagoproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yagomodel.Hash{word1, word2},
		Abstracts:   yagoproto.SearchAbstractsAuto,
	})

	if resp.Count != 1 {
		t.Errorf("Count = %d, want 1", resp.Count)
	}
	if len(resp.IndexAbstract) == 0 {
		t.Error("IndexAbstract empty, want reported term")
	}
}

func TestEndpointReportsRequestedTerms(t *testing.T) {
	word := hashFor("w1")
	index := fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
		word: {postingEntry(word, "u1", 0, 1), postingEntry(word, "u2", 0, 1)},
	}}
	endpoint := newEndpoint(index, fakeDirectory{})

	resp := serveSearch(t, endpoint, yagoproto.SearchRequest{
		NetworkName: "freeworld",
		Abstracts:   yagoproto.SearchAbstracts(word.String()),
	})

	if resp.IndexCount[word] != 2 {
		t.Errorf("IndexCount = %v, want 2 for term", resp.IndexCount)
	}
}

func TestEndpointRejectsWrongNetwork(t *testing.T) {
	endpoint := newEndpoint(fakeScanner{}, fakeDirectory{})

	resp := serveSearch(t, endpoint, yagoproto.SearchRequest{NetworkName: "othernet"})

	if resp.Count != 0 {
		t.Errorf("Count = %d, want 0 on network mismatch", resp.Count)
	}
}
