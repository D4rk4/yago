package rankfit

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type staticSearcher struct {
	results []searchcore.Result
	err     error
}

func (s staticSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	if s.err != nil {
		return searchcore.Response{}, s.err
	}

	return searchcore.Response{Results: s.results}, nil
}

// titleThresholdFactory ranks the relevant URL first only once Title clears the
// threshold, so a higher Title yields a better NDCG — a gradient the learner can
// climb.
func titleThresholdFactory(threshold float64) SearcherFactory {
	good := searchcore.Result{URL: "https://good.example/"}
	bad := searchcore.Result{URL: "https://bad.example/"}

	return func(w searchindex.RankingWeights) searchcore.Searcher {
		if w.Title >= threshold {
			return staticSearcher{results: []searchcore.Result{good, bad}}
		}

		return staticSearcher{results: []searchcore.Result{bad, good}}
	}
}

func relevanceJudgments() []searcheval.Judgment {
	return []searcheval.Judgment{{
		Query:    "q",
		Relevant: map[string]int{"https://good.example/": 1},
	}}
}

// fitOptions scores NDCG@2 so the two-document fixtures rank fully.
func fitOptions() Options {
	opts := DefaultOptions()
	opts.K = 2

	return opts
}

func TestFitImprovesNDCG(t *testing.T) {
	start := searchindex.DefaultRankingWeights() // Title 6
	report, err := Fit(
		t.Context(),
		start,
		relevanceJudgments(),
		titleThresholdFactory(8),
		fitOptions(),
	)
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	if !report.Improved() {
		t.Fatalf(
			"Fit did not improve NDCG: before=%v after=%v",
			report.BeforeNDCG,
			report.AfterNDCG,
		)
	}
	if report.AfterNDCG != 1 {
		t.Errorf("AfterNDCG = %v, want the relevant URL ranked first (1.0)", report.AfterNDCG)
	}
	if report.After.Title < 8 {
		t.Errorf("After.Title = %v, want it lifted past the threshold", report.After.Title)
	}
	if report.Rounds == 0 {
		t.Errorf("Rounds = 0, want at least one sweep")
	}
}

func TestFitRejectsNoJudgments(t *testing.T) {
	_, err := Fit(
		t.Context(),
		searchindex.DefaultRankingWeights(),
		nil,
		titleThresholdFactory(8),
		fitOptions(),
	)
	if err == nil {
		t.Fatalf("Fit with no judgments must fail")
	}
}

func TestFitPropagatesSearcherError(t *testing.T) {
	boom := errors.New("searcher down")
	factory := func(searchindex.RankingWeights) searchcore.Searcher {
		return staticSearcher{err: boom}
	}

	_, err := Fit(
		t.Context(),
		searchindex.DefaultRankingWeights(),
		relevanceJudgments(),
		factory,
		fitOptions(),
	)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the searcher error", err)
	}
}

func TestFitPropagatesAscendError(t *testing.T) {
	// The searcher scores the start weights but fails on any changed candidate,
	// so the start evaluation succeeds and the error surfaces inside Ascend.
	start := searchindex.DefaultRankingWeights()
	boom := errors.New("candidate down")
	factory := func(w searchindex.RankingWeights) searchcore.Searcher {
		if w.Title == start.Title {
			return staticSearcher{results: []searchcore.Result{{URL: "https://good.example/"}}}
		}

		return staticSearcher{err: boom}
	}

	_, err := Fit(t.Context(), start, relevanceJudgments(), factory, fitOptions())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the candidate searcher error from Ascend", err)
	}
}

func TestReportImproved(t *testing.T) {
	if (Report{BeforeNDCG: 0.5, AfterNDCG: 0.9}).Improved() != true {
		t.Errorf("a higher AfterNDCG must report improved")
	}
	if (Report{BeforeNDCG: 0.9, AfterNDCG: 0.9}).Improved() != false {
		t.Errorf("an equal AfterNDCG must not report improved")
	}
}
