package yagonode

import (
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func lexicalRankingWeights(
	current func() searchindex.RankingWeights,
) func() searchcore.LexicalRankingWeights {
	return func() searchcore.LexicalRankingWeights {
		if current == nil {
			return searchcore.DefaultLexicalRankingWeights()
		}
		weights := current()

		return searchcore.LexicalRankingWeights{
			Blend:        weights.LexicalBlend,
			GapAgreement: weights.LexicalGapAgreement,
		}
	}
}
