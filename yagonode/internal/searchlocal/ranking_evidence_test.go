package searchlocal

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestLocalRankingEvidenceCarriesKnownSignals(t *testing.T) {
	evidence := localRankingEvidence(searchcore.Request{Terms: []string{"one", "two"}},
		searchindex.SearchResult{
			Score: 2, StrictScore: 3, StrictRank: 1, RelaxedScore: 4, RelaxedRank: 2,
			Quality: 0.5, QualityKnown: true, SpamRisk: 0.25,
			FunctionWordFraction: 0.2, SymbolFraction: 0.1,
			AlphabeticFraction: 0.8, UniqueTokenFraction: 0.7,
			DateConfidence: 0.9, Proximity: 0.6, OrderedProximity: 0.4,
			FieldScores: map[string]float64{
				"title": 5, "headings": 4, "anchors": 3, "url": 2, "body": 1,
				"unknown": 99,
			},
		})
	want := map[searchcore.RankingSignal]float64{
		searchcore.SignalRetrievalScore:       2,
		searchcore.SignalStrictScore:          3,
		searchcore.SignalStrictRank:           1,
		searchcore.SignalRelaxedScore:         4,
		searchcore.SignalRelaxedRank:          2,
		searchcore.SignalQuality:              0.5,
		searchcore.SignalQualityKnown:         1,
		searchcore.SignalSpamRisk:             0.25,
		searchcore.SignalFunctionWordFraction: 0.2,
		searchcore.SignalSymbolFraction:       0.1,
		searchcore.SignalAlphabeticFraction:   0.8,
		searchcore.SignalUniqueTokenFraction:  0.7,
		searchcore.SignalDateConfidence:       0.9,
		searchcore.SignalUnorderedProximity:   0.6,
		searchcore.SignalOrderedProximity:     0.4,
		searchcore.SignalTitleScore:           5,
		searchcore.SignalHeadingScore:         4,
		searchcore.SignalAnchorScore:          3,
		searchcore.SignalURLScore:             2,
		searchcore.SignalBodyScore:            1,
	}
	for signal, expected := range want {
		value, known := evidence.Value(signal)
		if !known || value != expected {
			t.Fatalf("signal %s = %v/%v, want %v", signal.Name(), value, known, expected)
		}
	}
}

func TestLocalRankingEvidenceKeepsUnknownSignalsMissing(t *testing.T) {
	evidence := localRankingEvidence(searchcore.Request{}, searchindex.SearchResult{Score: 1})
	if value, known := evidence.Value(searchcore.SignalQualityKnown); !known || value != 0 {
		t.Fatalf("quality known = %v/%v", value, known)
	}
	for _, signal := range []searchcore.RankingSignal{
		searchcore.SignalQuality,
		searchcore.SignalDateConfidence,
		searchcore.SignalUnorderedProximity,
		searchcore.SignalOrderedProximity,
		searchcore.SignalStrictRank,
		searchcore.SignalRelaxedRank,
	} {
		if _, known := evidence.Value(signal); known {
			t.Fatalf("signal %s unexpectedly known", signal.Name())
		}
	}
}

func TestFieldRankingSignalAndBoolValue(t *testing.T) {
	if _, ok := fieldRankingSignal("unknown"); ok {
		t.Fatal("unknown field was accepted")
	}
	if boolRankingValue(true) != 1 || boolRankingValue(false) != 0 {
		t.Fatal("boolean ranking values are invalid")
	}
}
