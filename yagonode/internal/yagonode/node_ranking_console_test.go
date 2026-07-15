package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/judgments"
	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type fakeRanker struct {
	report rankfit.Report
	err    error
}

func (f fakeRanker) Tune(context.Context) (rankfit.Report, error) { return f.report, f.err }

type fakeCurated struct {
	count int
	err   error
}

func (f fakeCurated) List(context.Context) ([]judgments.Judgment, error) {
	return make([]judgments.Judgment, f.count), f.err
}

func TestNewRankingConsoleNilHolder(t *testing.T) {
	t.Parallel()

	if newRankingConsole(nil, fakeRanker{}, fakeCurated{}) != nil {
		t.Fatal("a nil holder must yield a nil source so the section renders unavailable")
	}
}

func TestRankingConsoleProfile(t *testing.T) {
	t.Parallel()

	holder := testRankingHolder(t)
	source := newRankingConsole(holder, fakeRanker{}, fakeCurated{count: 4})

	profile := source.Profile(context.Background())
	if profile.JudgmentCount != 4 || !profile.JudgmentsAvailable {
		t.Fatalf("judgment count = %d, want 4", profile.JudgmentCount)
	}
	definitions := searchindex.RankingWeightDefinitions()
	if len(profile.Weights) != len(definitions) {
		t.Fatalf("weight count = %d, want %d", len(profile.Weights), len(definitions))
	}
	for index, weight := range profile.Weights {
		definition := definitions[index]
		current, ok := holder.Current().Value(weight.Key)
		if !ok || weight.Key != definition.Key || weight.Label != definition.Label ||
			weight.Group != definition.Group || weight.Value != current {
			t.Fatalf("%s value = %v, want %v", weight.Key, weight.Value, current)
		}
		if weight.Maximum <= weight.Minimum || weight.Default < weight.Minimum ||
			weight.Default > weight.Maximum {
			t.Fatalf("%s bounds = %+v", weight.Key, weight)
		}
	}
}

func TestRankingConsoleJudgmentAvailability(t *testing.T) {
	t.Parallel()

	holder := testRankingHolder(t)
	missing := newRankingConsole(
		holder,
		fakeRanker{},
		nil,
	).Profile(context.Background())
	if missing.JudgmentsAvailable {
		t.Fatal("nil judgment store should be unavailable")
	}
	failing := newRankingConsole(holder, fakeRanker{}, fakeCurated{err: errors.New("boom")})
	if got := failing.Profile(context.Background()); got.JudgmentsAvailable {
		t.Fatal("errored judgment store should be unavailable")
	}
	empty := newRankingConsole(holder, fakeRanker{}, fakeCurated{}).Profile(context.Background())
	if empty.JudgmentCount != 0 || !empty.JudgmentsAvailable {
		t.Fatalf("empty judgment store = %+v, want available zero", empty)
	}
}

func TestRankingConsoleApplyPersistsValidWeights(t *testing.T) {
	t.Parallel()

	holder := testRankingHolder(t)
	source := newRankingConsole(holder, fakeRanker{}, fakeCurated{})
	before := holder.Current().Body

	if err := source.Apply(context.Background(), map[string]float64{"title": 9}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if holder.Current().Title != 9 {
		t.Fatalf("title = %v, want 9", holder.Current().Title)
	}
	// An omitted key keeps its current value (overlay, not replace).
	if holder.Current().Body != before {
		t.Fatalf("body changed to %v, want %v", holder.Current().Body, before)
	}
	if err := source.Apply(context.Background(), map[string]float64{"unknown": 1}); err != nil {
		t.Fatalf("unknown overlay: %v", err)
	}
	if holder.Current().Body != before {
		t.Fatalf("unknown overlay changed profile: %+v", holder.Current())
	}
}

func TestRankingConsoleApplyRejectsInvalidWeights(t *testing.T) {
	t.Parallel()

	holder := testRankingHolder(t)
	source := newRankingConsole(holder, fakeRanker{}, fakeCurated{})

	if err := source.Apply(context.Background(), map[string]float64{"quality": -1}); err == nil {
		t.Fatal("a negative prior must fail validation")
	}
	if holder.Current().Quality < 0 {
		t.Fatal("an invalid apply must not persist")
	}
}

func TestWeightsViewMarksLegacyValuesAboveCurrentWriteRange(t *testing.T) {
	weights := searchindex.DefaultRankingWeights()
	weights.Title = 65
	for _, weight := range weightsView(weights) {
		if weight.Key == "title" {
			if !weight.OutOfRange || weight.Value != 65 || weight.Maximum != 64 {
				t.Fatalf("legacy title view = %+v", weight)
			}

			return
		}
	}
	t.Fatal("title control missing")
}

func TestRankingConsoleTuneMapsReport(t *testing.T) {
	t.Parallel()

	after := searchindex.DefaultRankingWeights()
	after.Title = 8
	source := newRankingConsole(testRankingHolder(t), fakeRanker{report: rankfit.Report{
		After:      after,
		BeforeNDCG: 0.5,
		AfterNDCG:  0.7,
		Rounds:     3,
	}}, fakeCurated{})

	result, err := source.Tune(context.Background())
	if err != nil {
		t.Fatalf("tune: %v", err)
	}
	if result.BeforeNDCG != 0.5 || result.AfterNDCG != 0.7 || result.Rounds != 3 ||
		!result.Improved {
		t.Fatalf("tune result = %+v", result)
	}
	for _, weight := range result.Proposed {
		if weight.Key == "title" && weight.Value != 8 {
			t.Fatalf("proposed title = %v, want 8", weight.Value)
		}
	}
}

func TestRankingConsoleTuneSurfacesError(t *testing.T) {
	t.Parallel()

	source := newRankingConsole(
		testRankingHolder(t),
		fakeRanker{err: errors.New("boom")},
		fakeCurated{},
	)
	if _, err := source.Tune(context.Background()); err == nil {
		t.Fatal("tune must surface the tuner error")
	}
}
