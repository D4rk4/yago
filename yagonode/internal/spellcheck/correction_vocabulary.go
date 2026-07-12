package spellcheck

import (
	"sort"
	"strings"
)

const (
	maximumVocabularyTerms            = 8_192
	maximumDeleteReferences           = 1 << 18
	maximumDeleteIndexBytes           = 16 << 20
	deleteIndexKeyRetainedBytes       = 128
	deleteIndexReferenceRetainedBytes = 16
)

type correctorLimits struct {
	vocabularyTerms  int
	deleteReferences int
	deleteBytes      int
}

type vocabularyTerm struct {
	term      string
	frequency int
}

func newCorrector(frequency map[string]int, limits correctorLimits) *Corrector {
	corrector := &Corrector{
		frequency:       make(map[string]int, min(max(0, limits.vocabularyTerms), len(frequency))),
		vocabulary:      make([]string, 0, min(max(0, limits.vocabularyTerms), len(frequency))),
		deleteIndex:     map[string][]int{},
		maxEditDistance: defaultMaxEditDistance,
	}
	terms := normalizedVocabulary(frequency)
	for _, candidate := range terms {
		if len(corrector.vocabulary) >= max(0, limits.vocabularyTerms) {
			break
		}
		variants := sortedDeleteVariants(candidate.term, corrector.maxEditDistance)
		references, retainedBytes := deleteIndexAddition(corrector.deleteIndex, variants)
		if corrector.deleteReferences+references > max(0, limits.deleteReferences) ||
			corrector.deleteBytes+retainedBytes > max(0, limits.deleteBytes) {
			break
		}
		term := strings.Clone(candidate.term)
		identifier := len(corrector.vocabulary)
		corrector.frequency[term] = candidate.frequency
		corrector.vocabulary = append(corrector.vocabulary, term)
		for _, variant := range variants {
			corrector.deleteIndex[variant] = append(corrector.deleteIndex[variant], identifier)
		}
		corrector.deleteReferences += references
		corrector.deleteBytes += retainedBytes
	}

	return corrector
}

func normalizedVocabulary(frequency map[string]int) []vocabularyTerm {
	normalized := make(map[string]int, len(frequency))
	for term, freq := range frequency {
		term = strings.ToLower(strings.TrimSpace(term))
		if !correctableTerm(term) || freq <= 0 {
			continue
		}
		normalized[strings.Clone(term)] += freq
	}
	terms := make([]vocabularyTerm, 0, len(normalized))
	for term, termFrequency := range normalized {
		terms = append(terms, vocabularyTerm{term: term, frequency: termFrequency})
	}
	sort.Slice(terms, func(left, right int) bool {
		if terms[left].frequency != terms[right].frequency {
			return terms[left].frequency > terms[right].frequency
		}

		return terms[left].term < terms[right].term
	})

	return terms
}

func sortedDeleteVariants(term string, maxEdits int) []string {
	variantSet := deleteVariants(term, maxEdits)
	variants := make([]string, 0, len(variantSet))
	for variant := range variantSet {
		variants = append(variants, variant)
	}
	sort.Strings(variants)

	return variants
}

func deleteIndexAddition(index map[string][]int, variants []string) (int, int) {
	retainedBytes := len(variants) * deleteIndexReferenceRetainedBytes
	for _, variant := range variants {
		if _, found := index[variant]; !found {
			retainedBytes += deleteIndexKeyRetainedBytes + len(variant)
		}
	}

	return len(variants), retainedBytes
}
