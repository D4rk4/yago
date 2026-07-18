package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestSearchExplanationDiagnosticsUseAuthoritativeEvidence(t *testing.T) {
	diagnostics := searchExplanationDiagnosticValues(searchcore.NewRankingEvidence(
		searchcore.RankingSignalValue{Signal: searchcore.SignalSpamRisk, Value: 0.2},
		searchcore.RankingSignalValue{Signal: searchcore.SignalGlobalProximity, Value: 0.7},
	))
	if diagnostics.qualityKnown || !diagnostics.spamRiskKnown || diagnostics.spamRisk != 0.2 ||
		!diagnostics.proximityKnown || diagnostics.proximity != 0.7 ||
		diagnostics.functionWordKnown || diagnostics.symbolKnown ||
		diagnostics.alphabeticKnown || diagnostics.uniqueTokenKnown {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestSearchExplanationDiagnosticsPreferStoredProximity(t *testing.T) {
	diagnostics := searchExplanationDiagnosticValues(searchcore.NewRankingEvidence(
		searchcore.RankingSignalValue{Signal: searchcore.SignalUnorderedProximity, Value: 0.4},
		searchcore.RankingSignalValue{Signal: searchcore.SignalGlobalProximity, Value: 0.8},
	))
	if !diagnostics.proximityKnown || diagnostics.proximity != 0.4 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}
