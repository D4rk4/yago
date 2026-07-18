package yagonode

import "github.com/D4rk4/yago/yagonode/internal/searchcore"

type searchExplanationDiagnostics struct {
	quality              float64
	qualityKnown         bool
	spamRisk             float64
	spamRiskKnown        bool
	functionWordFraction float64
	functionWordKnown    bool
	symbolFraction       float64
	symbolKnown          bool
	alphabeticFraction   float64
	alphabeticKnown      bool
	uniqueTokenFraction  float64
	uniqueTokenKnown     bool
	proximity            float64
	proximityKnown       bool
}

func searchExplanationDiagnosticValues(
	evidence searchcore.RankingEvidence,
) searchExplanationDiagnostics {
	diagnostics := searchExplanationDiagnostics{}
	diagnostics.quality, diagnostics.qualityKnown = evidence.Value(searchcore.SignalQuality)
	diagnostics.spamRisk, diagnostics.spamRiskKnown = evidence.Value(searchcore.SignalSpamRisk)
	diagnostics.functionWordFraction, diagnostics.functionWordKnown = evidence.Value(
		searchcore.SignalFunctionWordFraction,
	)
	diagnostics.symbolFraction, diagnostics.symbolKnown = evidence.Value(
		searchcore.SignalSymbolFraction,
	)
	diagnostics.alphabeticFraction, diagnostics.alphabeticKnown = evidence.Value(
		searchcore.SignalAlphabeticFraction,
	)
	diagnostics.uniqueTokenFraction, diagnostics.uniqueTokenKnown = evidence.Value(
		searchcore.SignalUniqueTokenFraction,
	)
	diagnostics.proximity, diagnostics.proximityKnown = evidence.Value(
		searchcore.SignalUnorderedProximity,
	)
	if !diagnostics.proximityKnown {
		diagnostics.proximity, diagnostics.proximityKnown = evidence.Value(
			searchcore.SignalGlobalProximity,
		)
	}

	return diagnostics
}
