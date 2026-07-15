package searchindex

import (
	"github.com/blevesearch/bleve/v2/analysis"
)

type storedPhraseTokenMatcher struct {
	analyzer           analysis.Analyzer
	terms              map[string][]rune
	independentAnchors [][]rune
}

func newStoredPhraseTokenMatcher(
	phrases []string,
	analyzer analysis.Analyzer,
) storedPhraseTokenMatcher {
	matcher := storedPhraseTokenMatcher{
		analyzer: analyzer,
		terms:    make(map[string][]rune),
	}
	for _, phrase := range phrases {
		terms := analyzedStoredPhraseTerms(phrase, analyzer)
		if len(terms) < 2 {
			continue
		}
		for _, term := range terms {
			if _, exists := matcher.terms[term.term]; exists {
				continue
			}
			runes := []rune(term.term)
			matcher.terms[term.term] = append([]rune(nil), runes[:min(3, len(runes))]...)
		}
	}

	return matcher
}

func (m storedPhraseTokenMatcher) enabled() bool {
	return len(m.terms) > 0
}

func (m *storedPhraseTokenMatcher) bindSearchTargets(targets map[string][]int) {
	m.independentAnchors = m.independentAnchors[:0]
	for term, anchor := range m.terms {
		if len(targets[term]) == 0 {
			m.independentAnchors = append(m.independentAnchors, anchor)
		}
	}
}

func (m storedPhraseTokenMatcher) independentlyMightMatch(token string) bool {
	for _, anchor := range m.independentAnchors {
		if len(anchor) == 0 || containsFoldedRunes(token, anchor) {
			return true
		}
	}

	return false
}

func (s *storedFieldScan) observePhraseToken(
	analyzedTokens analysis.TokenStream,
	start int,
	end int,
	arrayIndex int,
	arrayLength int,
) {
	matcher := s.matcher.quotedPhrases
	if !matcher.enabled() {
		return
	}
	for _, analyzed := range analyzedTokens {
		term := string(analyzed.Term)
		if _, sought := matcher.terms[term]; !sought {
			continue
		}
		location := newStoredLocation(storedLocationCoordinates{
			position:    s.phrasePosition + storedLocationCoordinate(analyzed.Position),
			start:       start,
			end:         end,
			arrayIndex:  arrayIndex,
			arrayLength: arrayLength,
		})
		s.evidence.phraseTerms[term] = appendStoredLocation(
			s.evidence.phraseTerms[term],
			location,
		)
	}
	s.phrasePosition++
}
