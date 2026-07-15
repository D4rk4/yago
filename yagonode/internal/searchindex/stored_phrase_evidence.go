package searchindex

import (
	"sort"
	"strings"

	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/search"
)

const storedQuotedPhraseMaximumBoost = 0.15

type storedPhraseTerm struct {
	term   string
	offset uint64
}

func storedEvidenceAnalyzer(name string) analysis.Analyzer {
	indexMapping := loadStemmingMapping()
	if indexMapping == nil {
		return nil
	}
	analyzer := indexMapping.AnalyzerNamed(name)
	if analyzer == nil {
		analyzer = indexMapping.AnalyzerNamed(standardTextAnalyzer)
	}

	return analyzer
}

func storedQuotedPhrasePreference(
	locations search.FieldTermLocationMap,
	phrases []string,
	analyzer analysis.Analyzer,
) float64 {
	matched := 0
	measured := 0
	for _, phrase := range phrases {
		terms := analyzedStoredPhraseTerms(phrase, analyzer)
		if len(terms) < 2 {
			continue
		}
		measured++
		if storedPhrasePresent(locations, terms) {
			matched++
		}
	}
	if measured == 0 {
		return 0
	}

	return float64(matched) / float64(measured)
}

func analyzedStoredPhraseTerms(phrase string, analyzer analysis.Analyzer) []storedPhraseTerm {
	if analyzer != nil {
		tokens := analyzer.Analyze([]byte(phrase))
		terms := make([]storedPhraseTerm, 0, len(tokens))
		firstPosition := 0
		for _, token := range tokens {
			if firstPosition == 0 {
				firstPosition = token.Position
			}
			terms = append(terms, storedPhraseTerm{
				term:   string(token.Term),
				offset: storedLocationCoordinate(token.Position - firstPosition),
			})
		}

		return terms
	}
	terms := make([]storedPhraseTerm, 0)
	for start, end := range rangeStoredTokens(phrase) {
		terms = append(terms, storedPhraseTerm{
			term:   strings.ToLower(phrase[start:end]),
			offset: uint64(len(terms)),
		})
	}

	return terms
}

func storedPhrasePresent(
	locations search.FieldTermLocationMap,
	terms []storedPhraseTerm,
) bool {
	for _, field := range locations {
		for _, first := range field[terms[0].term] {
			present := true
			for _, term := range terms[1:] {
				if !storedPhraseLocationPresent(
					field[term.term],
					first.Pos+term.offset,
					first.ArrayPositions,
				) {
					present = false
					break
				}
			}
			if present {
				return true
			}
		}
	}

	return false
}

func storedPhraseLocationPresent(
	locations []*search.Location,
	position uint64,
	arrayPositions search.ArrayPositions,
) bool {
	for _, location := range locations {
		if location.Pos == position && location.ArrayPositions.Equals(arrayPositions) {
			return true
		}
	}

	return false
}

func rescoreStoredQuotedPhrasePrefix(results []SearchResult, req SearchRequest) {
	if len(req.Phrases) == 0 {
		return
	}
	prefix := results[:min(len(results), maximumSearchEvidenceResults)]
	for index := range prefix {
		prefix[index].Score *= 1 +
			storedQuotedPhraseMaximumBoost*prefix[index].quotedPhrasePreference
		prefix[index].quotedPhrasePreference = 0
	}
	sort.SliceStable(prefix, func(left int, right int) bool {
		return prefix[left].Score > prefix[right].Score
	})
}
