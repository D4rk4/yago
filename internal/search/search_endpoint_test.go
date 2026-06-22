package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type fixedStatus struct{}

func (fixedStatus) Snapshot(context.Context) StatusSnapshot {
	return StatusSnapshot{Version: "1.0", Uptime: 7}
}

func searchIdentity() httpguard.LocalPeer {
	return httpguard.LocalPeer{Hash: yacymodel.WordHash("self"), NetworkName: "freeworld"}
}

func newEndpoint(index fakeScanner, urls fakeDirectory) http.Handler {
	guard := httpguard.NewRequestGuard(
		searchIdentity(),
		httpguard.DefaultMaxBodyBytes,
		time.Second,
	)

	return New(guard, fixedStatus{}, index, urls, 100).Endpoint
}

func serveSearch(
	t *testing.T,
	endpoint http.Handler,
	req yacyproto.SearchRequest,
) yacyproto.SearchResponse {
	t.Helper()

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yacyproto.PathSearch,
		nil,
	)
	httpReq.PostForm = req.Form()
	endpoint.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	message, err := yacymodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	resp, err := yacyproto.ParseSearchResponse(message)
	if err != nil {
		t.Fatalf("ParseSearchResponse: %v", err)
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

	if resp.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", resp.Version)
	}
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
