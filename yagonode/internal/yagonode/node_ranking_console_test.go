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
	if profile.JudgmentCount != 4 {
		t.Fatalf("judgment count = %d, want 4", profile.JudgmentCount)
	}
	if len(profile.Weights) != len(rankingWeightMeta) {
		t.Fatalf("weight count = %d, want %d", len(profile.Weights), len(rankingWeightMeta))
	}
	current := weightsToMap(holder.Current())
	seen := map[string]bool{}
	for _, weight := range profile.Weights {
		seen[weight.Key] = true
		if weight.Value != current[weight.Key] {
			t.Fatalf("%s value = %v, want %v", weight.Key, weight.Value, current[weight.Key])
		}
		switch weight.Key {
		case "title":
			if weight.Label != "Title" || weight.Group != "Field boosts" {
				t.Fatalf("title meta = %q/%q", weight.Label, weight.Group)
			}
		case "proximity":
			if weight.Label != "Proximity (SDM)" || weight.Group != "Priors" {
				t.Fatalf("proximity meta = %q/%q", weight.Label, weight.Group)
			}
		}
	}
	if !seen["title"] || !seen["proximity"] {
		t.Fatalf("expected weights missing: %v", seen)
	}
}

func TestRankingConsoleJudgmentCountDegradesToZero(t *testing.T) {
	t.Parallel()

	holder := testRankingHolder(t)
	// A missing store and a read error both degrade to zero rather than failing.
	if got := newRankingConsole(
		holder,
		fakeRanker{},
		nil,
	).Profile(context.Background()); got.JudgmentCount != 0 {
		t.Fatalf("nil store count = %d, want 0", got.JudgmentCount)
	}
	failing := newRankingConsole(holder, fakeRanker{}, fakeCurated{err: errors.New("boom")})
	if got := failing.Profile(context.Background()); got.JudgmentCount != 0 {
		t.Fatalf("errored store count = %d, want 0", got.JudgmentCount)
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
