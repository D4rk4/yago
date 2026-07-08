package yagonode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type searchExplainScript struct {
	result searchindex.SearchResultSet
	err    error
	got    searchindex.SearchRequest
}

func (s *searchExplainScript) Index(context.Context, documentstore.Document) error { return nil }

func (s *searchExplainScript) Delete(context.Context, string) error { return nil }

func (s *searchExplainScript) Search(
	_ context.Context,
	req searchindex.SearchRequest,
) (searchindex.SearchResultSet, error) {
	s.got = req

	return s.result, s.err
}

func (s *searchExplainScript) Stats(context.Context) (searchindex.IndexStats, error) {
	return searchindex.IndexStats{}, nil
}

func postExplain(
	t *testing.T,
	index searchindex.SearchIndex,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		pathSearchExplain,
		strings.NewReader(body),
	)
	newSearchExplainEndpoint(index).ServeHTTP(rec, req)

	return rec
}

func TestSearchExplainEndpointRejectsNonPost(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSearchExplain, nil)
	newSearchExplainEndpoint(&searchExplainScript{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestSearchExplainEndpointRejectsBadRequests(t *testing.T) {
	cases := []struct {
		name   string
		index  searchindex.SearchIndex
		body   string
		status int
	}{
		{"nil index", nil, `{"query":"x"}`, http.StatusServiceUnavailable},
		{"bad json", &searchExplainScript{}, `{`, http.StatusBadRequest},
		{"empty query", &searchExplainScript{}, `{"query":"  "}`, http.StatusBadRequest},
		{
			"invalid weights",
			&searchExplainScript{},
			`{"query":"x","weights":{"title":-1}}`,
			http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		if rec := postExplain(t, tc.index, tc.body); rec.Code != tc.status {
			t.Fatalf("%s: status = %d, want %d", tc.name, rec.Code, tc.status)
		}
	}
}

func TestSearchExplainEndpointReturnsScoredResults(t *testing.T) {
	index := &searchExplainScript{result: searchindex.SearchResultSet{
		Results: []searchindex.SearchResult{
			{
				URL:         "https://a.example/",
				Score:       2.5,
				Explanation: "score 2.5",
				FieldScores: map[string]float64{"title": 1.5, "body": 1.0},
			},
		},
	}}
	body := `{"query":"alpha","weights":{"title":5,"headings":1,"anchors":1,"body":1,"url":1}}`
	rec := postExplain(t, index, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !index.got.Explain {
		t.Fatal("index search was not asked to explain")
	}
	if index.got.Weights.Title != 5 {
		t.Fatalf("weights not forwarded: %#v", index.got.Weights)
	}

	var resp searchExplainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) != 1 ||
		resp.Results[0].Explanation != "score 2.5" ||
		resp.Weights.Title != 5 {
		t.Fatalf("response = %#v", resp)
	}
	if resp.Results[0].FieldScores["title"] != 1.5 {
		t.Fatalf("per-field scores not surfaced: %#v", resp.Results[0].FieldScores)
	}
}

func TestSearchExplainEndpointDefaultsWeightsAndPropagatesError(t *testing.T) {
	index := &searchExplainScript{}
	if rec := postExplain(t, index, `{"query":"alpha"}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if index.got.Weights != searchindex.DefaultRankingWeights() {
		t.Fatalf("default weights not applied: %#v", index.got.Weights)
	}

	failing := &searchExplainScript{err: errors.New("boom")}
	if rec := postExplain(
		t,
		failing,
		`{"query":"alpha"}`,
	); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
