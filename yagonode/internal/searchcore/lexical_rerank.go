package searchcore

import (
	"context"
	"fmt"
	"sort"
)

const (
	// lexicalRerankWindow bounds the rerank to the ranks a searcher actually
	// reads, matching the MMR window.
	lexicalRerankWindow           = 50
	minimumRankingCandidateWindow = 50
	maximumRankingCandidateWindow = 100
	rankingCandidateMultiplier    = 5
)

// NewLexicalRerankSearcher reranks the merged top window of a searcher's results
// by a learning-free lexical signal — query-term coverage and proximity over the
// title and snippet — blended gently with the retrieval score. It runs above the
// federated merge so both local and remote results compete on the same textual
// evidence the user sees.
func NewLexicalRerankSearcher(inner Searcher) Searcher {
	return NewFinalRankingSearcher(NewLexicalEvidenceSearcher(inner))
}

func NewLexicalRerankSearcherWithWeights(
	inner Searcher,
	weights func() LexicalRankingWeights,
) Searcher {
	return NewFinalRankingSearcher(NewLexicalEvidenceSearcherWithWeights(inner, weights))
}

func NewLexicalEvidenceSearcher(inner Searcher) Searcher {
	return NewLexicalEvidenceSearcherWithWeights(inner, nil)
}

func NewLexicalEvidenceSearcherWithWeights(
	inner Searcher,
	weights func() LexicalRankingWeights,
) Searcher {
	return lexicalEvidenceSearcher{inner: inner, weights: weights}
}

func NewFinalRankingSearcher(inner Searcher) Searcher {
	return finalRankingSearcher{inner: inner}
}

type lexicalEvidenceSearcher struct {
	inner   Searcher
	weights func() LexicalRankingWeights
}

func (s lexicalEvidenceSearcher) Search(
	ctx context.Context,
	req Request,
) (Response, error) {
	response, err := s.inner.Search(ctx, rankingCandidateRequest(req))
	if err != nil {
		return Response{}, fmt.Errorf("lexical rerank inner search: %w", err)
	}
	if !req.SortByDate {
		response.Results = rerankLexicalProximityWithWeights(
			response.Results,
			req,
			lexicalRankingWeights(s.weights),
		)
	}
	response.Request = req

	return response, nil
}

type finalRankingSearcher struct {
	inner Searcher
}

func (s finalRankingSearcher) Search(
	ctx context.Context,
	req Request,
) (Response, error) {
	response, err := s.inner.Search(ctx, req)
	if err != nil {
		return Response{}, fmt.Errorf("final ranking inner search: %w", err)
	}
	response = responseSatisfyingDomainConstraints(req, response)
	response.Results = DiversifyResults(response.Results, req)
	OrderByDateWhenRequested(response.Results, req)
	response.Results = offsetResults(response.Results, req.Offset, rankingResultLimit(req))
	finalizeRankingPayload(response.Results, req.Explain)
	response.Request = req

	return response, nil
}

func rankingCandidateRequest(req Request) Request {
	window := req
	window.Offset = 0
	window.RankingFeatures = true
	requested := req.Offset + rankingResultLimit(req)
	if requested >= maximumRankingCandidateWindow {
		window.Limit = requested

		return window
	}
	window.Limit = min(
		maximumRankingCandidateWindow,
		max(minimumRankingCandidateWindow, requested*rankingCandidateMultiplier),
	)

	return window
}

func rankingResultLimit(req Request) int {
	if req.Limit <= 0 {
		return DefaultPublicLimit
	}

	return req.Limit
}

func rerankLexicalProximity(results []Result, req Request) []Result {
	return rerankLexicalProximityWithWeights(
		results,
		req,
		DefaultLexicalRankingWeights(),
	)
}

func rerankLexicalProximityWithWeights(
	results []Result,
	req Request,
	weights LexicalRankingWeights,
) []Result {
	window := min(len(results), lexicalRerankWindow)
	if window < 3 {
		return results
	}
	top := results[:window]
	maxScore := top[0].Score
	for _, result := range top {
		maxScore = max(maxScore, result.Score)
	}

	keys := make([]float64, window)
	coverages := make([]float64, window)
	proximities := make([]float64, window)
	ordered := make([]float64, window)
	for i, result := range top {
		requirements := rerankResultRequirements(req, result)
		terms := rerankRequirementTerms(requirements)
		gapAgreement := 0.0
		normScore := 0.0
		if maxScore > 0 {
			normScore = max(0, result.Score/maxScore)
		}
		if len(terms) > 0 {
			coverage, proximity, orderedValue, gapValue := lexicalDependenceComponents(
				result,
				terms,
				requirements,
			)
			coverages[i] = coverage
			proximities[i] = proximity
			ordered[i] = orderedValue
			gapAgreement = gapValue
		}
		lexical := (coverages[i] + proximities[i] + ordered[i]) / 3
		lexical += weights.GapAgreement * gapAgreement * (1 - lexical)
		keys[i] = (1-weights.Blend)*normScore + weights.Blend*lexical
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
		result := top[index]
		result.Evidence = result.Evidence.With(SignalTermCoverage, coverages[index])
		result.Evidence = result.Evidence.With(SignalGlobalProximity, proximities[index])
		result.Evidence = result.Evidence.With(SignalOrderedProximity, ordered[index])
		result = WithDiversityRelevance(result, keys[index])
		reranked = append(reranked, result)
	}

	return append(reranked, results[window:]...)
}

// lexicalSignal scores a result's query-term coverage and proximity, preferring
// the document's matched-term positions (local results, RANK-ENABLER) so the
// measure spans the whole document, and falling back to the title-plus-snippet
// text for remote results, which carry no positions.
func lexicalSignal(result Result, terms []string) float64 {
	coverage, proximity := lexicalComponents(result, terms)

	return (coverage + proximity) / 2
}

func lexicalComponents(result Result, terms []string) (float64, float64) {
	if coverage, proximity, ok := lexicalComponentsFromPositions(
		result.FieldTermPositions,
		terms,
	); ok {
		return coverage, proximity
	}
	if result.EvidenceReady {
		return 0, 0
	}

	return lexicalTextComponents(result.Title+" "+result.Snippet, terms)
}

// lexicalScoreFromPositions scores coverage and proximity from per-field matched
// positions: coverage counts a query term present in any field, proximity is the
// tightest single-field minimum window (proximity across fields is meaningless).
// It reports ok=false when no field carries a matched query term — an empty map,
// or only stemmed location keys that do not equal the raw query terms — so the
// caller falls back to the snippet.
func lexicalScoreFromPositions(
	fieldPositions map[string]map[string][]int,
	terms []string,
) (float64, bool) {
	coverage, proximity, ok := lexicalComponentsFromPositions(fieldPositions, terms)

	return (coverage + proximity) / 2, ok
}

func lexicalComponentsFromPositions(
	fieldPositions map[string]map[string][]int,
	terms []string,
) (float64, float64, bool) {
	if len(fieldPositions) == 0 {
		return 0, 0, false
	}
	termIndex := make(map[string]int, len(terms))
	for i, term := range terms {
		termIndex[term] = i
	}
	covered := map[int]bool{}
	bestProximity := 0.0
	for _, termPositions := range fieldPositions {
		hitTerm, hitPos := fieldHits(termPositions, termIndex, covered)
		proximity := proximityScore(hitTerm, hitPos, distinctTerms(hitTerm))
		bestProximity = max(bestProximity, proximity)
	}
	if len(covered) == 0 {
		return 0, 0, false
	}

	return float64(len(covered)) / float64(len(terms)), bestProximity, true
}

// fieldHits flattens one field's term→positions into position-sorted parallel
// (termIndex, position) slices for the proximity sweep, recording every matched
// term in covered for the cross-field coverage count.
func fieldHits(
	termPositions map[string][]int,
	termIndex map[string]int,
	covered map[int]bool,
) ([]int, []int) {
	type hit struct{ term, pos int }
	hits := make([]hit, 0, len(termPositions))
	for term, positions := range termPositions {
		index, ok := termIndex[term]
		if !ok || len(positions) == 0 {
			continue
		}
		covered[index] = true
		for _, position := range positions {
			hits = append(hits, hit{term: index, pos: position})
		}
	}
	sort.Slice(hits, func(a, b int) bool { return hits[a].pos < hits[b].pos })
	hitTerm := make([]int, len(hits))
	hitPos := make([]int, len(hits))
	for i, entry := range hits {
		hitTerm[i] = entry.term
		hitPos[i] = entry.pos
	}

	return hitTerm, hitPos
}

// distinctTerms counts the distinct term indices in a hit sequence.
func distinctTerms(hitTerm []int) int {
	seen := make(map[int]bool, len(hitTerm))
	for _, term := range hitTerm {
		seen[term] = true
	}

	return len(seen)
}

// lexicalScore is the mean of term coverage (share of query terms present) and
// proximity (closeness of the matched terms), both in [0,1].
func lexicalScore(text string, terms []string) float64 {
	coverage, proximity := lexicalTextComponents(text, terms)

	return (coverage + proximity) / 2
}

func lexicalTextComponents(text string, terms []string) (float64, float64) {
	if len(terms) == 0 {
		return 0, 0
	}
	index := make(map[string]int, len(terms))
	for i, term := range terms {
		index[term] = i
	}
	matched := map[int]bool{}
	hitTerm, hitPos := fieldHits(lexicalTextTermPositions(text, terms), index, matched)
	coverage := float64(len(matched)) / float64(len(terms))

	return coverage, proximityScore(hitTerm, hitPos, len(matched))
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
