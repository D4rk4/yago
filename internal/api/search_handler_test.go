package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func TestSearchHandlerHappyPath(t *testing.T) {
	h := newTestHarness(t)
	wordHash := testHash(t, "word")
	h.searcher.result = contracts.SearchResult{
		Resources:  []yacymodel.URIMetadataRow{sampleURLRow(t)},
		JoinCount:  9,
		SearchTime: 12 * time.Millisecond,
		References: []string{"alpha", "beta"},
		WordCounts: map[yacymodel.Hash]int{wordHash: 3},
		Abstracts:  map[yacymodel.Hash]string{wordHash: "abc"},
	}

	req := yacyproto.SearchRequest{
		Query:      []yacymodel.Hash{wordHash},
		Count:      10,
		Time:       1200,
		MaxDist:    4,
		Partitions: 30,
		Abstracts:  yacyproto.SearchAbstractsAuto,
		ContentDom: yacyproto.ContentDomainAll,
		Language:   "en",
	}
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
	if h.searcher.query.MaxTime != 1200*time.Millisecond {
		t.Errorf("MaxTime = %s, want 1200ms", h.searcher.query.MaxTime)
	}
	if h.searcher.query.Abstracts.Mode != contracts.SearchAbstractAuto {
		t.Errorf("Abstracts = %+v, want auto", h.searcher.query.Abstracts)
	}
	if h.searcher.query.Filters.Language != "en" ||
		h.searcher.query.Filters.ContentDomain != "" ||
		h.searcher.query.Filters.Partitions != 30 {
		t.Errorf("filters = %+v", h.searcher.query.Filters)
	}
	if resp.JoinCount != 9 {
		t.Errorf("JoinCount = %d, want 9", resp.JoinCount)
	}
	if resp.SearchTime != 12 {
		t.Errorf("SearchTime = %d, want 12", resp.SearchTime)
	}
	if resp.References != "alpha,beta" {
		t.Errorf("References = %q, want alpha,beta", resp.References)
	}
	if resp.Count != 1 {
		t.Errorf("Count = %d, want 1", resp.Count)
	}
	if resp.IndexCount[wordHash] != 3 {
		t.Errorf("IndexCount = %v", resp.IndexCount)
	}
}

func TestSearchHandlerDefaultsCountAndTime(t *testing.T) {
	h := newTestHarness(t)
	wordHash := testHash(t, "word")
	req := yacyproto.SearchRequest{Query: []yacymodel.Hash{wordHash}}
	h.do(t, http.MethodPost, yacyproto.PathSearch, req.Form())

	if h.searcher.query.MaxResults != defaultSearchCount {
		t.Errorf("MaxResults = %d, want %d", h.searcher.query.MaxResults, defaultSearchCount)
	}
	if h.searcher.query.MaxTime != defaultSearchTime {
		t.Errorf("MaxTime = %s, want %s", h.searcher.query.MaxTime, defaultSearchTime)
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

func TestSearchHandlerRejectsUnsupportedSearchOption(t *testing.T) {
	h := newTestHarness(t)
	req := yacyproto.SearchRequest{
		Query:    []yacymodel.Hash{testHash(t, "word")},
		Modifier: "/language/de",
	}
	rec := h.do(t, http.MethodPost, yacyproto.PathSearch, req.Form())

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %q", rec.Code, rec.Body.String())
	}
	if h.searcher.called {
		t.Fatal("searcher must not be called")
	}
}
