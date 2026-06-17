package api

import (
	"net/http"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func TestSearchHandlerHappyPath(t *testing.T) {
	h := newTestHarness(t)
	wordHash := testHash(t, "word")
	h.searcher.result = core.SearchResult{
		Resources:  []yacymodel.URIMetadataRow{sampleURLRow(t)},
		JoinCount:  9,
		WordCounts: map[yacymodel.Hash]int{wordHash: 3},
		Abstracts:  map[yacymodel.Hash]string{wordHash: "abc"},
	}

	req := yacyproto.SearchRequest{Query: []yacymodel.Hash{wordHash}, Count: 10, MaxDist: 4}
	rec := h.do(t, http.MethodPost, yacyproto.PathSearch, req.Form())

	resp, err := yacyproto.ParseSearchResponse(decodeResponse(t, rec))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !h.searcher.called {
		t.Fatal("searcher not called")
	}
	if h.searcher.query.MaxResults != 10 || h.searcher.query.MaxDistance != 4 {
		t.Errorf("query honored fields = %+v", h.searcher.query)
	}
	if resp.JoinCount != 9 {
		t.Errorf("JoinCount = %d, want 9", resp.JoinCount)
	}
	if resp.Count != 1 {
		t.Errorf("Count = %d, want 1", resp.Count)
	}
	if resp.IndexCount[wordHash] != 3 {
		t.Errorf("IndexCount = %v", resp.IndexCount)
	}
}

func TestSearchHandlerWrongNetwork(t *testing.T) {
	h := newTestHarness(t)
	req := yacyproto.SearchRequest{NetworkName: "othernet"}
	rec := h.do(t, http.MethodPost, yacyproto.PathSearch, req.Form())

	decodeResponse(t, rec)
	if h.searcher.called {
		t.Fatal("searcher must not be called on network mismatch")
	}
}
