package searchcore

import (
	"math"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagonode/internal/stopwords"
)

type pseudoRelevanceDocument struct {
	rank          int
	score         float64
	length        int
	termFrequency map[string]int
}

type pseudoRelevanceTerm struct {
	probability         float64
	feedbackProbability float64
	documents           int
}

type pseudoRelevanceCandidate struct {
	term        string
	probability float64
	documents   int
}

func minePseudoRelevanceTerms(
	results []Result,
	queryTerms []string,
	excludedTerms []string,
) []string {
	model := buildPseudoRelevanceModel(results, queryTerms)
	blocked := pseudoRelevanceBlockedTerms(queryTerms, excludedTerms)
	candidates := make([]pseudoRelevanceCandidate, 0, len(model))
	for term, evidence := range model {
		if blocked[term] || stopwords.IsStopword(term) ||
			utf8.RuneCountInString(term) < prfMinTermLen ||
			evidence.documents < prfMinFeedbackDocFreq {
			continue
		}
		candidates = append(candidates, pseudoRelevanceCandidate{
			term:        term,
			probability: evidence.probability,
			documents:   evidence.documents,
		})
	}
	sort.Slice(candidates, func(left, right int) bool {
		return pseudoRelevanceCandidateLess(candidates[left], candidates[right])
	})
	if len(candidates) > prfExpansionTerms {
		candidates = candidates[:prfExpansionTerms]
	}
	expansion := make([]string, len(candidates))
	for index, candidate := range candidates {
		expansion[index] = candidate.term
	}

	return expansion
}

func pseudoRelevanceCandidateLess(
	left pseudoRelevanceCandidate,
	right pseudoRelevanceCandidate,
) bool {
	if left.probability != right.probability {
		return left.probability > right.probability
	}
	if left.documents != right.documents {
		return left.documents > right.documents
	}

	return left.term < right.term
}

func buildPseudoRelevanceModel(
	results []Result,
	queryTerms []string,
) map[string]pseudoRelevanceTerm {
	documents := pseudoRelevanceDocuments(results)
	if len(documents) < prfMinFeedbackDocFreq {
		return nil
	}
	posteriors := pseudoRelevanceDocumentPosteriors(documents)
	model := make(map[string]pseudoRelevanceTerm)
	for index, document := range documents {
		for term, frequency := range document.termFrequency {
			evidence := model[term]
			evidence.feedbackProbability += posteriors[index] *
				float64(frequency) / float64(document.length)
			evidence.documents++
			model[term] = evidence
		}
	}
	queryTokens := pseudoRelevanceTermTokens(queryTerms, prfMaximumQueryTokens)
	originalWeight := prfOriginalQueryWeight
	if len(queryTokens) == 0 {
		originalWeight = 0
	}
	for term, evidence := range model {
		evidence.probability = (1 - originalWeight) * evidence.feedbackProbability
		model[term] = evidence
	}
	queryFrequency := pseudoRelevanceTermFrequency(queryTokens)
	for term, frequency := range queryFrequency {
		evidence := model[term]
		evidence.probability += originalWeight * float64(frequency) / float64(len(queryTokens))
		model[term] = evidence
	}

	return model
}

func pseudoRelevanceDocuments(results []Result) []pseudoRelevanceDocument {
	limit := min(len(results), prfFeedbackDocs)
	documents := make([]pseudoRelevanceDocument, 0, limit)
	seen := make(map[string]struct{}, limit)
	for index, result := range results[:limit] {
		identity := pseudoRelevanceDocumentIdentity(result)
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		tokens := pseudoRelevanceTokens(result.Title, prfMaximumDocumentTokens)
		remaining := prfMaximumDocumentTokens - len(tokens)
		tokens = append(tokens, pseudoRelevanceTokens(result.Snippet, remaining)...)
		if len(tokens) == 0 {
			continue
		}
		documents = append(documents, pseudoRelevanceDocument{
			rank:          index + 1,
			score:         result.Score,
			length:        len(tokens),
			termFrequency: pseudoRelevanceTermFrequency(tokens),
		})
	}

	return documents
}

func pseudoRelevanceDocumentIdentity(result Result) string {
	if result.ClusterID != "" {
		return "cluster:" + result.ClusterID
	}

	return resultIdentity(result)
}

func pseudoRelevanceDocumentPosteriors(
	documents []pseudoRelevanceDocument,
) []float64 {
	minimumScore := 0.0
	maximumScore := 0.0
	haveFiniteScore := false
	for _, document := range documents {
		if math.IsNaN(document.score) || math.IsInf(document.score, 0) {
			continue
		}
		if !haveFiniteScore {
			minimumScore = document.score
			maximumScore = document.score
			haveFiniteScore = true
			continue
		}
		minimumScore = min(minimumScore, document.score)
		maximumScore = max(maximumScore, document.score)
	}
	posteriors := make([]float64, len(documents))
	total := 0.0
	for index, document := range documents {
		normalizedScore := 0.0
		if haveFiniteScore && maximumScore > minimumScore &&
			!math.IsNaN(document.score) && !math.IsInf(document.score, 0) {
			normalizedScore = (document.score - minimumScore) / (maximumScore - minimumScore)
		}
		posteriors[index] = (1 + normalizedScore) / float64(document.rank)
		total += posteriors[index]
	}
	for index := range posteriors {
		posteriors[index] /= total
	}

	return posteriors
}

func pseudoRelevanceQueryTerms(request Request) []string {
	if len(request.Terms) != 0 {
		return request.Terms
	}

	return []string{request.Query}
}

func pseudoRelevanceBlockedTerms(queryTerms, excludedTerms []string) map[string]bool {
	blocked := make(map[string]bool)
	for _, term := range pseudoRelevanceTermTokens(queryTerms, prfMaximumQueryTokens) {
		blocked[term] = true
	}
	for _, term := range pseudoRelevanceTermTokens(excludedTerms, prfMaximumQueryTokens) {
		blocked[term] = true
	}

	return blocked
}

func pseudoRelevanceTermTokens(terms []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	tokens := make([]string, 0, min(len(terms), limit))
	for _, term := range terms {
		remaining := limit - len(tokens)
		if remaining == 0 {
			break
		}
		tokens = append(tokens, pseudoRelevanceTokens(term, remaining)...)
	}

	return tokens
}

func pseudoRelevanceTermFrequency(tokens []string) map[string]int {
	frequency := make(map[string]int, len(tokens))
	for _, token := range tokens {
		frequency[token]++
	}

	return frequency
}

func pseudoRelevanceTokens(text string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	tokens := make([]string, 0, min(limit, 16))
	term := make([]rune, 0, prfMaximumTermRunes)
	discardTerm := false
	appendTerm := func() {
		if len(term) != 0 && !discardTerm {
			tokens = append(tokens, string(term))
		}
		term = term[:0]
		discardTerm = false
	}
	for _, character := range text {
		wordCharacter := unicode.IsLetter(character) || unicode.IsDigit(character) ||
			unicode.IsMark(character) && (len(term) != 0 || discardTerm)
		if !wordCharacter {
			appendTerm()
			if len(tokens) == limit {
				return tokens
			}
			continue
		}
		if discardTerm {
			continue
		}
		if len(term) == prfMaximumTermRunes {
			term = term[:0]
			discardTerm = true
			continue
		}
		term = append(term, unicode.ToLower(character))
	}
	appendTerm()

	return tokens
}
