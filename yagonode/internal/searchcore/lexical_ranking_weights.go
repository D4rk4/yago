package searchcore

import "math"

type LexicalRankingWeights struct {
	Blend        float64
	GapAgreement float64
}

func DefaultLexicalRankingWeights() LexicalRankingWeights {
	return LexicalRankingWeights{Blend: 0.25, GapAgreement: 0.05}
}

func lexicalRankingWeights(
	provider func() LexicalRankingWeights,
) LexicalRankingWeights {
	if provider == nil {
		return DefaultLexicalRankingWeights()
	}
	weights := provider()
	if math.IsNaN(weights.Blend) || math.IsInf(weights.Blend, 0) ||
		math.IsNaN(weights.GapAgreement) || math.IsInf(weights.GapAgreement, 0) {
		return DefaultLexicalRankingWeights()
	}
	weights.Blend = min(1, max(0, weights.Blend))
	weights.GapAgreement = min(1, max(0, weights.GapAgreement))

	return weights
}
