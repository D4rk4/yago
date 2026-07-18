package yagonode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

type searchExplainScript struct {
	result     searchindex.SearchResultSet
	err        error
	got        searchindex.SearchRequest
	requests   []searchindex.SearchRequest
	honorLimit bool
}

func (s *searchExplainScript) Index(context.Context, documentstore.Document) error { return nil }

func (s *searchExplainScript) Delete(context.Context, string) error { return nil }

func (s *searchExplainScript) Search(
	_ context.Context,
	req searchindex.SearchRequest,
) (searchindex.SearchResultSet, error) {
	s.got = req
	s.requests = append(s.requests, req)
	result := s.result
	if s.honorLimit && len(result.Results) > req.MaxResults {
		result.Results = append([]searchindex.SearchResult(nil), result.Results[:req.MaxResults]...)
	}

	return result, s.err
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
	newSearchExplainEndpoint(index, nil, nil, nil, nil).ServeHTTP(rec, req)

	return rec
}

func TestSearchExplainEndpointRejectsNonPost(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSearchExplain, nil)
	newSearchExplainEndpoint(&searchExplainScript{}, nil, nil, nil, nil).ServeHTTP(rec, req)
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
				URL:                  "https://a.example/",
				Score:                2.5,
				Quality:              0.7,
				QualityKnown:         true,
				SpamRisk:             0.2,
				FunctionWordFraction: 0.3,
				SymbolFraction:       0.1,
				AlphabeticFraction:   0.8,
				UniqueTokenFraction:  0.6,
				Proximity:            0.5,
				StrictRank:           1,
				StrictScore:          2.5,
				Explanation:          "score 2.5",
				FieldScores:          map[string]float64{"title": 1.5, "body": 1.0},
			},
		},
	}}
	body := `{"query":"near \"alpha beta\"","weights":{"title":5,"headings":1,"anchors":1,"body":1,"url":1,"quality":1}}`
	rec := postExplain(t, index, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !index.got.Explain {
		t.Fatal("index search was not asked to explain")
	}
	if index.got.Query != "alpha beta" || len(index.got.Terms) != 2 ||
		!index.got.Near || !index.got.IncludePositions {
		t.Fatalf("parsed index request = %#v", index.got)
	}
	if index.got.MaxResults != 50 {
		t.Fatalf("candidate window = %d, want 50", index.got.MaxResults)
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
		resp.Weights.Title != 5 || resp.Scope != searchcore.SourceLocal {
		t.Fatalf("response = %#v", resp)
	}
	if resp.Results[0].FieldScores["title"] != 1.5 {
		t.Fatalf("per-field scores not surfaced: %#v", resp.Results[0].FieldScores)
	}
	if resp.Results[0].Quality != 0.7 {
		t.Fatalf("quality prior not surfaced: %v", resp.Results[0].Quality)
	}
	if !resp.Results[0].QualityKnown || !resp.Results[0].SpamRiskKnown ||
		!resp.Results[0].FunctionWordKnown || !resp.Results[0].SymbolKnown ||
		!resp.Results[0].AlphabeticKnown || !resp.Results[0].UniqueTokenKnown ||
		resp.Results[0].SpamRisk != 0.2 ||
		resp.Results[0].FunctionWordFraction != 0.3 ||
		resp.Results[0].SymbolFraction != 0.1 ||
		resp.Results[0].AlphabeticFraction != 0.8 ||
		resp.Results[0].UniqueTokenFraction != 0.6 {
		t.Fatalf("quality diagnostics not surfaced: %#v", resp.Results[0])
	}
	if !resp.Results[0].ProximityKnown || resp.Results[0].Proximity != 0.5 {
		t.Fatalf("proximity feature not surfaced: %v", resp.Results[0].Proximity)
	}
	strictRank := false
	for _, signal := range resp.Results[0].Evidence {
		strictRank = strictRank || signal == (searchExplainSignal{Name: "strict_rank", Value: 1})
	}
	if !strictRank {
		t.Fatalf("ranking evidence not surfaced: %#v", resp.Results[0].Evidence)
	}
	if resp.Results[0].Score <= 2.5 {
		t.Fatalf("production priors not applied to score: %v", resp.Results[0].Score)
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
	live := searchindex.DefaultRankingWeights()
	live.Title = 9
	recorder := httptest.NewRecorder()
	newSearchExplainEndpoint(
		index,
		func() searchindex.RankingWeights { return live },
		nil,
		nil,
		nil,
	).ServeHTTP(
		recorder,
		httptest.NewRequestWithContext(
			t.Context(), http.MethodPost, pathSearchExplain,
			strings.NewReader(`{"query":"alpha"}`),
		),
	)
	if recorder.Code != http.StatusOK || index.got.Weights != live {
		t.Fatalf("live weights response = %d, %#v", recorder.Code, index.got.Weights)
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

func TestSearchExplainEndpointAppliesServingDenylist(t *testing.T) {
	index := &searchExplainScript{result: searchindex.SearchResultSet{
		Total: 2,
		Results: []searchindex.SearchResult{
			{URL: "https://blocked.example/", Score: 2},
			{URL: "https://allowed.example/", Score: 1},
		},
	}}
	deny := openDenylistStore(t, map[urldenylist.Kind][]string{
		urldenylist.KindDomain: {"blocked.example"},
	})
	recorder := httptest.NewRecorder()
	newSearchExplainEndpoint(index, nil, nil, nil, deny).ServeHTTP(
		recorder,
		httptest.NewRequestWithContext(
			t.Context(), http.MethodPost, pathSearchExplain,
			strings.NewReader(`{"query":"alpha"}`),
		),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response searchExplainResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].URL != "https://allowed.example/" {
		t.Fatalf("denylisted explain results = %#v", response.Results)
	}
}

func TestSearchExplainEndpointAppliesAndExplainsLearnedModel(t *testing.T) {
	index := &searchExplainScript{result: searchindex.SearchResultSet{
		Results: []searchindex.SearchResult{
			{URL: "https://low.example/", Score: 1},
			{URL: "https://high.example/", Score: 3},
		},
	}}
	ranker := activeExplainRanker(t)
	recorder := httptest.NewRecorder()
	newSearchExplainEndpoint(index, nil, nil, ranker, nil).ServeHTTP(
		recorder,
		httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			pathSearchExplain,
			strings.NewReader(`{"query":"alpha"}`),
		),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", recorder.Code, recorder.Body.String())
	}
	var response searchExplainResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ModelRevision != "explain-v1" ||
		response.ModelKind != learnedrank.ModelLinearLambdaRank ||
		len(response.Results) != 2 || response.Results[0].URL != "https://high.example/" ||
		response.Results[0].Learned == nil || len(response.Results[0].Learned.Signals) == 0 {
		t.Fatalf("learned response = %#v", response)
	}

	index.result.Results[0].Score = math.MaxFloat64
	recorder = httptest.NewRecorder()
	newSearchExplainEndpoint(index, nil, nil, ranker, nil).ServeHTTP(
		recorder,
		httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			pathSearchExplain,
			strings.NewReader(`{"query":"alpha"}`),
		),
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("invalid evidence status = %d", recorder.Code)
	}
}

func TestSearchExplainEndpointUsesServingCandidateWindow(t *testing.T) {
	results := make([]searchindex.SearchResult, 60)
	for index := range results {
		results[index] = searchindex.SearchResult{
			URL:   fmt.Sprintf("https://candidate.example/%02d", index),
			Score: float64(60 - index),
		}
	}
	results[len(results)-1].Score = 100
	index := &searchExplainScript{
		result:     searchindex.SearchResultSet{Results: results},
		honorLimit: true,
	}
	recorder := httptest.NewRecorder()
	newSearchExplainEndpoint(index, nil, nil, activeExplainRanker(t), nil).ServeHTTP(
		recorder,
		httptest.NewRequestWithContext(
			t.Context(), http.MethodPost, pathSearchExplain,
			strings.NewReader(`{"query":"alpha"}`),
		),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", recorder.Code, recorder.Body.String())
	}
	var response searchExplainResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if index.got.MaxResults != learnedrank.DefaultCandidateWindow ||
		len(response.Results) != searchExplainMaxResults ||
		response.Results[0].URL != "https://candidate.example/59" {
		t.Fatalf("candidate explain response = %#v, request = %#v", response, index.got)
	}
}

func TestSearchExplainEndpointUsesPseudoRelevanceFeedback(t *testing.T) {
	index := &searchExplainScript{
		result: searchindex.SearchResultSet{Results: []searchindex.SearchResult{
			{
				URL: "https://feedback.example/one", Title: "alpha sharedtopic reference",
			},
			{
				URL: "https://feedback.example/two", Title: "alpha sharedtopic guide",
			},
		}},
	}
	recorder := httptest.NewRecorder()
	newSearchExplainEndpoint(index, nil, nil, nil, nil).ServeHTTP(
		recorder,
		httptest.NewRequestWithContext(
			t.Context(), http.MethodPost, pathSearchExplain,
			strings.NewReader(`{"query":"alpha"}`),
		),
	)
	if recorder.Code != http.StatusOK || len(index.requests) != 2 ||
		!slices.Contains(index.requests[1].ExpansionTerms, "sharedtopic") {
		t.Fatalf("feedback response = %d, requests = %#v", recorder.Code, index.requests)
	}
}

func TestLearnedExplainIdentityAndIndexing(t *testing.T) {
	explanation := learnedrank.ResultExplanation{Identity: "url:url"}
	indexed := learnedExplanationsByIdentity([]learnedrank.ResultExplanation{explanation})
	if indexed["url:url"] == nil {
		t.Fatalf("indexed explanations = %#v", indexed)
	}
	cases := []struct {
		result searchcore.Result
		want   string
	}{
		{searchcore.Result{URLHash: "hash", URL: "url"}, "hash:hash"},
		{searchcore.Result{URL: "url"}, "url:url"},
		{searchcore.Result{DisplayURL: "display"}, "display_url:display"},
		{searchcore.Result{Title: "title"}, "title:title"},
		{searchcore.Result{}, ""},
	}
	for _, test := range cases {
		if got := learnedResultIdentity(test.result); got != test.want {
			t.Fatalf("identity = %q, want %q", got, test.want)
		}
	}
}

func activeExplainRanker(t *testing.T) *learnedrank.Ranker {
	t.Helper()
	definitions := learnedrank.FeatureDefinitions()
	weights := make([]float64, len(definitions))
	weights[0] = 1
	model, err := rankfit.NewLinearLambdaRankModel(definitions, weights)
	if err != nil {
		t.Fatalf("NewLinearLambdaRankModel: %v", err)
	}
	snapshot, err := learnedrank.NewLinearSnapshot("explain-v1", model)
	if err != nil {
		t.Fatalf("NewLinearSnapshot: %v", err)
	}
	ranker, err := learnedrank.NewRanker(100)
	if err != nil {
		t.Fatalf("NewRanker: %v", err)
	}
	if err := ranker.Activate(snapshot); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	return ranker
}
