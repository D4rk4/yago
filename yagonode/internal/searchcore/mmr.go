package searchcore

import "strings"

const (
	// mmrLambda balances relevance against novelty (Carbonell & Goldstein,
	// SIGIR 1998): 0.7 keeps ranking mostly relevance-driven while pushing
	// texts that repeat an already-chosen result down the page.
	mmrLambda = 0.7
	// mmrWindow bounds the O(n²) similarity comparisons to the ranks a
	// searcher actually reads.
	mmrWindow = 50
)

// rerankMarginalRelevance reorders the top window with Maximal Marginal
// Relevance: each step picks the result maximizing
// λ·relevance − (1−λ)·maxSimilarity to the already-picked set, where
// relevance is the (RRF-fused) score normalized into [0,1] and similarity is
// token Jaccard over title+snippet. Results beyond the window keep their
// order; the tail follows unchanged.
func rerankMarginalRelevance(results []Result) []Result {
	window := min(len(results), mmrWindow)
	if window < 3 {
		return results
	}
	top := results[:window]
	tokens := make([]map[string]bool, window)
	for i, result := range top {
		tokens[i] = tokenSet(result.Title + " " + result.Snippet)
	}
	scale := top[0].Score
	if scale <= 0 {
		scale = 1
	}

	picked := make([]Result, 0, window)
	pickedTokens := make([]map[string]bool, 0, window)
	remaining := make([]int, window)
	for i := range remaining {
		remaining[i] = i
	}
	for len(remaining) > 0 {
		best := 0
		bestValue := mmrValue(top, tokens, pickedTokens, remaining[0], scale)
		for i := 1; i < len(remaining); i++ {
			if value := mmrValue(
				top,
				tokens,
				pickedTokens,
				remaining[i],
				scale,
			); value > bestValue {
				best = i
				bestValue = value
			}
		}
		index := remaining[best]
		picked = append(picked, top[index])
		pickedTokens = append(pickedTokens, tokens[index])
		remaining = append(remaining[:best], remaining[best+1:]...)
	}

	return append(picked, results[window:]...)
}

func mmrValue(
	top []Result,
	tokens []map[string]bool,
	picked []map[string]bool,
	index int,
	scale float64,
) float64 {
	relevance := top[index].Score / scale
	maxSimilarity := 0.0
	for _, chosen := range picked {
		if similarity := jaccard(tokens[index], chosen); similarity > maxSimilarity {
			maxSimilarity = similarity
		}
	}

	return mmrLambda*relevance - (1-mmrLambda)*maxSimilarity
}

func tokenSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, token := range strings.Fields(strings.ToLower(text)) {
		if len(token) >= 3 {
			set[token] = true
		}
	}

	return set
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	small, large := a, b
	if len(b) < len(a) {
		small, large = b, a
	}
	shared := 0
	for token := range small {
		if large[token] {
			shared++
		}
	}

	return float64(shared) / float64(len(a)+len(b)-shared)
}
