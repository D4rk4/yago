package searchcore

import (
	"strconv"
	"testing"
)

var lexicalWindowBenchmarkResult string

func BenchmarkLexicalRerankPositionWindow(b *testing.B) {
	terms := []string{"alpha", "rare-identifier", "beta", "gamma"}
	results := make([]Result, lexicalRerankWindow)
	for candidate := range results {
		positions := make(map[string][]int, len(terms))
		for termIndex, term := range terms {
			locations := make([]int, 12)
			for occurrence := range locations {
				locations[occurrence] = candidate + termIndex*2 + occurrence*17 + 1
			}
			positions[term] = locations
		}
		results[candidate] = Result{
			URL:                "https://example" + strconv.Itoa(candidate) + ".org/document",
			Score:              float64(lexicalRerankWindow - candidate),
			EvidenceReady:      true,
			FieldTermPositions: bodyPositions(positions),
		}
	}
	req := Request{Terms: terms}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		reranked := rerankLexicalProximity(results, req)
		lexicalWindowBenchmarkResult = reranked[0].URL
	}
}
