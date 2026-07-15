package searchcore

import (
	"fmt"
	"testing"
)

func BenchmarkLexicalRankingWeightProvider(b *testing.B) {
	results := make([]Result, lexicalRerankWindow)
	for index := range results {
		results[index] = Result{
			URL:     fmt.Sprintf("https://host-%d.example/page", index),
			Score:   float64(len(results) - index),
			Title:   "alpha beta gamma",
			Snippet: "alpha filler beta filler gamma",
		}
	}
	request := Request{Terms: []string{"alpha", "beta", "gamma"}}
	weights := DefaultLexicalRankingWeights()
	b.Run("fixed", func(b *testing.B) {
		for range b.N {
			rerankLexicalProximityWithWeights(results, request, weights)
		}
	})
	b.Run("live-provider", func(b *testing.B) {
		provider := func() LexicalRankingWeights { return weights }
		for range b.N {
			rerankLexicalProximityWithWeights(
				results,
				request,
				lexicalRankingWeights(provider),
			)
		}
	})
}
