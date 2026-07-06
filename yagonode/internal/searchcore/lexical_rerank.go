package searchcore

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const (
	// lexicalRerankWindow bounds the rerank to the ranks a searcher actually
	// reads, matching the MMR window.
	lexicalRerankWindow = 50
	// lexicalRerankWeight is the share of the reorder key taken from the lexical
	// signal; the rest stays with the retrieval score. Kept low so the reranker
	// only breaks near-ties — it lifts a result whose query terms are all present
	// and close together over one that merely mentions them, without overriding
	// BM25/RRF retrieval order.
	lexicalRerankWeight = 0.25
)

// NewLexicalRerankSearcher reranks the merged top window of a searcher's results
// by a learning-free lexical signal — query-term coverage and proximity over the
// title and snippet — blended gently with the retrieval score. It runs above the
// federated merge so both local and remote results compete on the same textual
// evidence the user sees.
func NewLexicalRerankSearcher(inner Searcher) Searcher {
	return lexicalRerankSearcher{inner: inner}
}

type lexicalRerankSearcher struct {
	inner Searcher
}

func (s lexicalRerankSearcher) Search(
	ctx context.Context,
	req Request,
) (Response, error) {
	response, err := s.inner.Search(ctx, req)
	if err != nil {
		return Response{}, fmt.Errorf("lexical rerank inner search: %w", err)
	}
	response.Results = rerankLexicalProximity(response.Results, req)

	return response, nil
}

// rerankLexicalProximity reorders the top window by (1−w)·normScore + w·lexical,
// where lexical is the mean of query-term coverage and proximity. The tail past
// the window keeps its order. It is a no-op for single-term queries, where
// coverage and proximity carry no signal the retrieval score does not.
func rerankLexicalProximity(results []Result, req Request) []Result {
	terms := rerankQueryTerms(req)
	window := min(len(results), lexicalRerankWindow)
	if window < 3 || len(terms) < 2 {
		return results
	}
	top := results[:window]
	minScore, maxScore := top[0].Score, top[0].Score
	for _, result := range top {
		minScore = min(minScore, result.Score)
		maxScore = max(maxScore, result.Score)
	}

	keys := make([]float64, window)
	for i, result := range top {
		normScore := 0.0
		if maxScore > minScore {
			normScore = (result.Score - minScore) / (maxScore - minScore)
		}
		lexical := lexicalScore(result.Title+" "+result.Snippet, terms)
		keys[i] = (1-lexicalRerankWeight)*normScore + lexicalRerankWeight*lexical
	}

	order := make([]int, window)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return keys[order[a]] > keys[order[b]]
	})
	reranked := make([]Result, 0, len(results))
	for _, index := range order {
		reranked = append(reranked, top[index])
	}

	return append(reranked, results[window:]...)
}

// rerankQueryTerms is the distinct lowercased query terms, preferring the parsed
// terms and falling back to whitespace splitting of the raw query.
func rerankQueryTerms(req Request) []string {
	raw := req.Terms
	if len(raw) == 0 {
		raw = strings.Fields(req.Query)
	}
	seen := map[string]bool{}
	terms := make([]string, 0, len(raw))
	for _, term := range raw {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}

	return terms
}

// lexicalScore is the mean of term coverage (share of query terms present) and
// proximity (closeness of the matched terms), both in [0,1].
func lexicalScore(text string, terms []string) float64 {
	index := map[string]int{}
	for i, term := range terms {
		index[term] = i
	}
	matched := map[int]bool{}
	hitTerm := make([]int, 0)
	hitPos := make([]int, 0)
	for position, token := range strings.Fields(strings.ToLower(text)) {
		if termIndex, ok := index[token]; ok {
			matched[termIndex] = true
			hitTerm = append(hitTerm, termIndex)
			hitPos = append(hitPos, position)
		}
	}
	coverage := float64(len(matched)) / float64(len(terms))

	return (coverage + proximityScore(hitTerm, hitPos, len(matched))) / 2
}

// proximityScore maps the smallest absolute token span covering every distinct
// matched term to [0,1]: adjacent terms score near 1, far-apart terms near 0. It
// is 0 when fewer than two distinct terms matched, since proximity needs a pair.
func proximityScore(hitTerm []int, hitPos []int, distinct int) float64 {
	if distinct < 2 {
		return 0
	}
	// Minimum-window sweep over the matched tokens; the span is measured in
	// absolute token positions so intervening non-query words widen it.
	counts := map[int]int{}
	covered := 0
	best := hitPos[len(hitPos)-1] - hitPos[0] + 1
	left := 0
	for right, term := range hitTerm {
		if counts[term] == 0 {
			covered++
		}
		counts[term]++
		for covered == distinct {
			if span := hitPos[right] - hitPos[left] + 1; span < best {
				best = span
			}
			counts[hitTerm[left]]--
			if counts[hitTerm[left]] == 0 {
				covered--
			}
			left++
		}
	}

	return float64(distinct) / float64(best)
}
