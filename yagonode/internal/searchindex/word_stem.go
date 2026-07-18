package searchindex

import (
	"strings"
	"sync"

	"github.com/blevesearch/bleve/v2/mapping"
)

// stemmingMappingOnce lazily builds a mapping that carries the registered
// per-language analyzers, so StemWord can resolve a stemmer by name without
// opening an index.
var (
	stemmingMappingOnce sync.Once
	stemmingMapping     *mapping.IndexMappingImpl
)

// loadStemmingMapping returns the analyzer-carrying mapping, building it once. It
// is a var so tests can force the degraded paths (a nil mapping, an unregistered
// analyzer) that a real, index-backed mapping never reaches. The registered
// analyzer set is identical to the search index mapping; a build failure leaves
// the mapping nil and StemWord falls back to folding.
var loadStemmingMapping = func() *mapping.IndexMappingImpl {
	stemmingMappingOnce.Do(func() {
		stemmingMapping, _ = newSearchIndexMapping()
	})

	return stemmingMapping
}

// StemWord reduces a word to the stem produced by the analyzer of its dominant
// script — the same per-language Snowball stemmer the index uses — so callers can
// group surface forms by shared stem without hardcoding any language's endings.
// A word in a script with no stemmer, or when analysis yields nothing, folds to
// lowercase unchanged.
func StemWord(word string) string {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return ""
	}

	return stemWordWithAnalyzer(word, queryAnalyzers(word)[0])
}

func stemWordWithAnalyzer(word string, analyzerName string) string {
	if analyzerName == standardTextAnalyzer || isCJKAnalyzer(analyzerName) {
		return normalizedUnstemmedWord(word, analyzerName)
	}
	indexMapping := loadStemmingMapping()
	if indexMapping == nil {
		return word
	}
	// queryAnalyzers only ever names an analyzer the built mapping registers, so a
	// non-nil mapping always resolves it.
	tokens := indexMapping.AnalyzerNamed(analyzerName).Analyze([]byte(word))
	if len(tokens) == 0 {
		return word
	}

	return string(tokens[0].Term)
}
