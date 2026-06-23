package search

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func searchIdentity() httpguard.PeerIdentity {
	return httpguard.PeerIdentity{Hash: yacymodel.WordHash("self"), NetworkName: "freeworld"}
}

func newEndpoint(
	index fakeScanner,
	urls fakeDirectory,
) func(context.Context, yacyproto.SearchRequest) (yacyproto.SearchResponse, error) {
	return searchEndpoint{
		peer: searchIdentity(),
		searcher: searcher{
			index:           index,
			urls:            urls,
			postingsPerWord: 100,
		},
	}.Serve
}

func serveSearch(
	t *testing.T,
	endpoint func(context.Context, yacyproto.SearchRequest) (yacyproto.SearchResponse, error),
	req yacyproto.SearchRequest,
) yacyproto.SearchResponse {
	t.Helper()

	resp, err := endpoint(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}

	return resp
}

func TestEndpointJoinsAndAnswers(t *testing.T) {
	word := hashFor("w1")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word: {postingEntry(word, "u1", 0, 1), postingEntry(word, "u2", 0, 1)},
	}}
	endpoint := newEndpoint(index, fakeDirectory{rows: urlRows("u1", "u2")})

	resp := serveSearch(t, endpoint, yacyproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yacymodel.Hash{word},
		Count:       10,
		Language:    "en",
	})

	if resp.Count != 2 || resp.JoinCount != 2 {
		t.Errorf("Count = %d, JoinCount = %d, want 2/2", resp.Count, resp.JoinCount)
	}
}

func TestEndpointAutoAbstractAndReferences(t *testing.T) {
	word1, word2 := hashFor("w1"), hashFor("w2")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word1: {postingEntry(word1, "u1", 0, 1), postingEntry(word1, "u2", 0, 1)},
		word2: {postingEntry(word2, "u2", 0, 1)},
	}}
	endpoint := newEndpoint(index, fakeDirectory{rows: urlRows("u1", "u2")})

	resp := serveSearch(t, endpoint, yacyproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yacymodel.Hash{word1, word2},
		Abstracts:   yacyproto.SearchAbstractsAuto,
	})

	if resp.Count != 1 {
		t.Errorf("Count = %d, want 1", resp.Count)
	}
	if len(resp.IndexAbstract) == 0 {
		t.Error("IndexAbstract empty, want auto abstract")
	}
}

func TestEndpointExplicitAbstract(t *testing.T) {
	word := hashFor("w1")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word: {postingEntry(word, "u1", 0, 1), postingEntry(word, "u2", 0, 1)},
	}}
	endpoint := newEndpoint(index, fakeDirectory{})

	resp := serveSearch(t, endpoint, yacyproto.SearchRequest{
		NetworkName: "freeworld",
		Abstracts:   yacyproto.SearchAbstracts(word.String()),
	})

	if resp.IndexCount[word] != 2 {
		t.Errorf("IndexCount = %v, want 2 for word", resp.IndexCount)
	}
}

func TestEndpointRejectsWrongNetwork(t *testing.T) {
	endpoint := newEndpoint(fakeScanner{}, fakeDirectory{})

	resp := serveSearch(t, endpoint, yacyproto.SearchRequest{NetworkName: "othernet"})

	if resp.Count != 0 {
		t.Errorf("Count = %d, want 0 on network mismatch", resp.Count)
	}
}
