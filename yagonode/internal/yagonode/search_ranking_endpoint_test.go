package yagonode

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/rankingprofile"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func testRankingHolder(t *testing.T) *rankingprofile.Holder {
	t.Helper()
	vault, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })

	holder, err := rankingprofile.Open(context.Background(), vault)
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}

	return holder
}

func serveRanking(
	t *testing.T,
	endpoint http.Handler,
	method, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, pathSearchRanking, reader)
	rec := httptest.NewRecorder()
	endpoint.ServeHTTP(rec, req)

	return rec
}

func TestSearchRankingEndpointGetReturnsCurrent(t *testing.T) {
	endpoint := newSearchRankingEndpoint(testRankingHolder(t))
	rec := serveRanking(t, endpoint, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body searchRankingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Weights != searchindex.DefaultRankingWeights() {
		t.Fatalf("weights = %+v, want default", body.Weights)
	}
}

func TestSearchRankingEndpointPostAppliesWeights(t *testing.T) {
	holder := testRankingHolder(t)
	endpoint := newSearchRankingEndpoint(holder)
	rec := serveRanking(
		t,
		endpoint,
		http.MethodPost,
		`{"weights":{"title":9,"headings":2,"anchors":2,"body":1,"url":3}}`,
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	want := searchindex.DefaultRankingWeights()
	want.Title = 9
	want.Headings = 2
	want.Anchors = 2
	want.Body = 1
	want.URL = 3
	if holder.Current() != want {
		t.Fatalf("current = %+v, want %+v", holder.Current(), want)
	}
}

func TestSearchRankingEndpointPostRejectsInvalid(t *testing.T) {
	endpoint := newSearchRankingEndpoint(testRankingHolder(t))
	rec := serveRanking(t, endpoint, http.MethodPost, `{"weights":{"hostRank":-1}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSearchRankingEndpointPostPreservesOmittedLiveWeights(t *testing.T) {
	holder := testRankingHolder(t)
	before := holder.Current()
	rec := serveRanking(
		t,
		newSearchRankingEndpoint(holder),
		http.MethodPost,
		`{"weights":{"title":7}}`,
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	after := holder.Current()
	if after.Title != 7 {
		t.Fatalf("title = %v, want 7", after.Title)
	}
	after.Title = before.Title
	if after != before {
		t.Fatalf("omitted weights changed: before=%+v after=%+v", before, after)
	}
}

func TestSearchRankingEndpointRejectsMalformed(t *testing.T) {
	endpoint := newSearchRankingEndpoint(testRankingHolder(t))
	rec := serveRanking(t, endpoint, http.MethodPost, `{`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSearchRankingEndpointMethodNotAllowed(t *testing.T) {
	endpoint := newSearchRankingEndpoint(testRankingHolder(t))
	rec := serveRanking(t, endpoint, http.MethodPut, "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") == "" {
		t.Fatal("Allow header missing")
	}
}

func TestSearchRankingEndpointUnavailable(t *testing.T) {
	endpoint := newSearchRankingEndpoint(nil)
	rec := serveRanking(t, endpoint, http.MethodGet, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
