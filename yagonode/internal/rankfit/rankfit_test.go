package rankfit

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestAscendRaisesRewardedWeightsFromZeroAndPositive(t *testing.T) {
	// The objective rewards Title (starts positive → multiplicative steps) and
	// HostRank (starts at zero → seed values), so both candidate branches run.
	start := searchindex.RankingWeights{
		Title: 1, Headings: 1, Anchors: 1, Body: 1, URL: 1, HostRank: 0, Freshness: 0,
	}
	objective := func(w searchindex.RankingWeights) (float64, error) {
		return w.Title + w.HostRank, nil
	}

	best, score, rounds, err := Ascend(start, objective, DefaultOptions())
	if err != nil {
		t.Fatalf("Ascend: %v", err)
	}
	if best.Title <= start.Title {
		t.Errorf("Title = %v, want it raised above %v", best.Title, start.Title)
	}
	if best.HostRank <= 0 {
		t.Errorf("HostRank = %v, want a seed value re-enabled it", best.HostRank)
	}
	if score <= start.Title {
		t.Errorf("score = %v, want it above the start score %v", score, start.Title)
	}
	if rounds == 0 {
		t.Errorf("rounds = 0, want at least one sweep")
	}
}

func TestAscendStopsWhenObjectivePlateaus(t *testing.T) {
	// min(Title, 3) plateaus once Title reaches 3, so a sweep eventually gains
	// nothing and the MinGain early-stop fires before MaxRounds. Only Title is
	// positive, so the zero candidate makes every field zero and is skipped by
	// the Validate guard.
	start := searchindex.RankingWeights{Title: 1}
	objective := func(w searchindex.RankingWeights) (float64, error) {
		return min(w.Title, 3.0), nil
	}

	best, score, rounds, err := Ascend(start, objective, DefaultOptions())
	if err != nil {
		t.Fatalf("Ascend: %v", err)
	}
	if rounds >= DefaultOptions().MaxRounds {
		t.Errorf("rounds = %d, want an early MinGain stop", rounds)
	}
	if score < 3 {
		t.Errorf("score = %v, want the plateau of 3 reached", score)
	}
	if best.Title <= 0 {
		t.Errorf("Title = %v, want the single field to stay positive (Validate guard)", best.Title)
	}
}

func TestAscendPropagatesStartObjectiveError(t *testing.T) {
	boom := errors.New("boom")
	objective := func(searchindex.RankingWeights) (float64, error) {
		return 0, boom
	}

	if _, _, _, err := Ascend(
		searchindex.DefaultRankingWeights(),
		objective,
		DefaultOptions(),
	); !errors.Is(
		err,
		boom,
	) {
		t.Fatalf("err = %v, want the start objective error", err)
	}
}

func TestAscendPropagatesMidSearchObjectiveError(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	objective := func(w searchindex.RankingWeights) (float64, error) {
		calls++
		if calls > 1 {
			return 0, boom
		}

		return w.Title, nil
	}

	if _, _, _, err := Ascend(
		searchindex.DefaultRankingWeights(),
		objective,
		DefaultOptions(),
	); !errors.Is(
		err,
		boom,
	) {
		t.Fatalf("err = %v, want the mid-search objective error", err)
	}
}
