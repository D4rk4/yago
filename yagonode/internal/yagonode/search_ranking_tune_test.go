package yagonode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/judgments"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type stubCurated struct {
	list []judgments.Judgment
	err  error
}

func (s stubCurated) List(context.Context) ([]judgments.Judgment, error) {
	return s.list, s.err
}

type stubImplicit struct {
	list []searcheval.Judgment
	err  error
}

func (s stubImplicit) ImplicitJudgments(
	context.Context,
	int,
) ([]searcheval.Judgment, error) {
	return s.list, s.err
}

func TestTrainingJudgmentsMergesImplicitFillingUncuratedQueries(t *testing.T) {
	tuner := rankingTuner{
		curated: linuxCurated(), // curated query "linux kernel"
		implicit: stubImplicit{list: []searcheval.Judgment{
			// Same query as curated: dropped, the human label wins wholesale.
			{Query: "linux kernel", Relevant: map[string]int{"https://spam.example/": 2}},
			// Uncurated query: added to the training set.
			{Query: "go generics", Relevant: map[string]int{"https://go.example/": 2}},
		}},
	}
	graded, err := tuner.trainingJudgments(t.Context())
	if err != nil {
		t.Fatalf("trainingJudgments: %v", err)
	}
	if len(graded) != 2 {
		t.Fatalf("got %d judgments, want 2 (curated + one implicit)", len(graded))
	}
	byQuery := map[string]searcheval.Judgment{}
	for _, judgment := range graded {
		byQuery[judgment.Query] = judgment
	}
	if _, spam := byQuery["linux kernel"].Relevant["https://spam.example/"]; spam {
		t.Error("an implicit judgment overrode a curated query")
	}
	if byQuery["go generics"].Relevant["https://go.example/"] != 2 {
		t.Error("an implicit judgment for an uncurated query was not added")
	}
}

func TestTrainingJudgmentsCuratedOnlyWhenImplicitNil(t *testing.T) {
	tuner := rankingTuner{curated: linuxCurated()}
	graded, err := tuner.trainingJudgments(t.Context())
	if err != nil {
		t.Fatalf("trainingJudgments: %v", err)
	}
	if len(graded) != 1 || graded[0].Query != "linux kernel" {
		t.Fatalf("curated-only training set = %+v", graded)
	}
}

func TestTrainingJudgmentsPropagatesImplicitError(t *testing.T) {
	tuner := rankingTuner{
		curated:  linuxCurated(),
		implicit: stubImplicit{err: errors.New("clicks down")},
	}
	if _, err := tuner.trainingJudgments(t.Context()); err == nil {
		t.Fatal("expected the implicit-source error to propagate")
	}
}

type stubTuneSearcher struct {
	err error
}

func (s stubTuneSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	if s.err != nil {
		return searchcore.Response{}, s.err
	}

	return searchcore.Response{}, nil
}

type stubStoredDocuments struct {
	docs []documentstore.Document
}

func (s stubStoredDocuments) StoredDocuments(
	_ context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	for _, doc := range s.docs {
		if _, err := visit(doc); err != nil {
			return err
		}
	}

	return nil
}

func linuxCurated() stubCurated {
	return stubCurated{list: []judgments.Judgment{{
		Query:  "linux kernel",
		Grades: map[string]int{"https://good.example/": 1},
	}}}
}

func testTuneIndex(t *testing.T) searchindex.SearchIndex {
	t.Helper()
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), stubStoredDocuments{
		docs: []documentstore.Document{
			{
				NormalizedURL: "https://good.example/",
				Title:         "linux kernel guide",
				ExtractedText: "the linux kernel scheduler and memory management",
			},
			{
				NormalizedURL: "https://bad.example/",
				Title:         "cooking recipes",
				ExtractedText: "pasta sauce and cheese",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	return index
}

func okFactory() func(searchindex.RankingWeights) searchcore.Searcher {
	return func(searchindex.RankingWeights) searchcore.Searcher { return stubTuneSearcher{} }
}

func newTestTuner(t *testing.T) rankingTuner {
	t.Helper()

	return newRankingTuner(
		testTuneIndex(t),
		func() hostrank.Table { return nil },
		testRankingHolder(t),
		linuxCurated(),
		nil,
	)
}

func TestTuneReportsMissingIndex(t *testing.T) {
	tuner := newRankingTuner(nil, nil, testRankingHolder(t), linuxCurated(), nil)
	if _, err := tuner.Tune(t.Context()); err == nil {
		t.Fatalf("Tune without an index must fail")
	}
}

func TestTuneReportsListError(t *testing.T) {
	tuner := rankingTuner{
		factory: okFactory(),
		ranking: testRankingHolder(t),
		curated: stubCurated{err: errors.New("store down")},
	}
	if _, err := tuner.Tune(t.Context()); err == nil {
		t.Fatalf("Tune must surface the list error")
	}
}

func TestTuneReportsNoJudgments(t *testing.T) {
	tuner := rankingTuner{
		factory: okFactory(),
		ranking: testRankingHolder(t),
		curated: stubCurated{},
	}
	if _, err := tuner.Tune(t.Context()); err == nil {
		t.Fatalf("Tune with no judgments must fail")
	}
}

func TestTuneReportsFitError(t *testing.T) {
	boom := errors.New("searcher down")
	tuner := rankingTuner{
		factory: func(searchindex.RankingWeights) searchcore.Searcher {
			return stubTuneSearcher{err: boom}
		},
		ranking: testRankingHolder(t),
		curated: linuxCurated(),
	}
	if _, err := tuner.Tune(t.Context()); err == nil {
		t.Fatalf("Tune must surface the fit error")
	}
}

func TestTuneEndToEndOverRealIndex(t *testing.T) {
	tuner := newTestTuner(t)

	report, err := tuner.Tune(t.Context())
	if err != nil {
		t.Fatalf("Tune: %v", err)
	}
	if report.AfterNDCG != 1 {
		t.Errorf("AfterNDCG = %v, want the relevant page ranked first", report.AfterNDCG)
	}
	if report.Rounds == 0 {
		t.Errorf("Rounds = 0, want at least one sweep")
	}
}

func TestTuneEndpointRejectsNonPost(t *testing.T) {
	endpoint := newSearchRankingTuneEndpoint(newTestTuner(t))
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSearchRankingTune, nil)
	endpoint.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", rec.Code)
	}
}

func TestTuneEndpointUnavailable(t *testing.T) {
	endpoint := newSearchRankingTuneEndpoint(rankingTuner{ranking: nil, curated: linuxCurated()})
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, pathSearchRankingTune, nil)
	endpoint.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestTuneEndpointReportsTuneError(t *testing.T) {
	// A nil factory (no index) passes the availability guard but fails in Tune.
	endpoint := newSearchRankingTuneEndpoint(
		newRankingTuner(nil, nil, testRankingHolder(t), linuxCurated(), nil),
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, pathSearchRankingTune, nil)
	endpoint.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestTuneEndpointReturnsPreview(t *testing.T) {
	endpoint := newSearchRankingTuneEndpoint(newTestTuner(t))
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, pathSearchRankingTune, nil)
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var got searchRankingTuneResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.AfterNDCG != 1 {
		t.Errorf("AfterNDCG = %v, want 1", got.AfterNDCG)
	}
}
