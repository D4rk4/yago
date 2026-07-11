package searchcore

import (
	"math"
	"reflect"
	"testing"
)

func TestRankingEvidenceIsCopyOnlyAndRejectsInvalidValues(t *testing.T) {
	original := NewRankingEvidence(
		RankingSignalValue{Signal: SignalStrictRank, Value: 2},
		RankingSignalValue{Signal: rankingSignalLimit, Value: 9},
		RankingSignalValue{Signal: SignalQuality, Value: math.NaN()},
	)
	changed := original.With(SignalStrictRank, 1).With(SignalSpamRisk, math.Inf(1))
	if value, known := original.Value(SignalStrictRank); !known || value != 2 {
		t.Fatalf("original strict rank = %v/%v", value, known)
	}
	if value, _ := changed.Value(SignalStrictRank); value != 1 {
		t.Fatalf("changed strict rank = %v", value)
	}
	for _, signal := range []RankingSignal{rankingSignalLimit, SignalQuality, SignalSpamRisk} {
		if _, known := original.Value(signal); known {
			t.Fatalf("invalid signal %d became known", signal)
		}
	}
}

func TestRankingEvidenceAddsOverlaysAndListsSignals(t *testing.T) {
	base := NewRankingEvidence(
		RankingSignalValue{Signal: SignalPeerSupport, Value: 1},
		RankingSignalValue{Signal: SignalQuality, Value: 0.5},
	)
	base = base.Add(SignalPeerSupport, 2).Add(SignalSourceCount, 1)
	overlay := NewRankingEvidence(
		RankingSignalValue{Signal: SignalQuality, Value: -1},
		RankingSignalValue{Signal: SignalSpamRisk, Value: 0.9},
	)
	got := base.Overlay(overlay)
	want := []RankingSignalValue{
		{Signal: SignalQuality, Value: 0.5},
		{Signal: SignalSpamRisk, Value: 0.9},
		{Signal: SignalPeerSupport, Value: 3},
		{Signal: SignalSourceCount, Value: 1},
	}
	if !reflect.DeepEqual(got.Values(), want) {
		t.Fatalf("values = %#v, want %#v", got.Values(), want)
	}
}

func TestRankingSignalNamesAreCompleteAndBounded(t *testing.T) {
	if len(rankingSignalNames) != int(rankingSignalLimit) {
		t.Fatalf("names = %d, signals = %d", len(rankingSignalNames), rankingSignalLimit)
	}
	seen := map[string]bool{}
	for signal := RankingSignal(0); signal < rankingSignalLimit; signal++ {
		name := signal.Name()
		if name == "" || seen[name] {
			t.Fatalf("signal %d name = %q", signal, name)
		}
		seen[name] = true
	}
	if got := rankingSignalLimit.Name(); got != "" {
		t.Fatalf("out-of-range name = %q", got)
	}
}
