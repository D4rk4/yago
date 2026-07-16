package searchcore

import (
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/querymatch"
)

type VisibleTextTerms struct {
	original string
	folded   string
	tokens   []string
}

func NewVisibleTextTerms(text string) VisibleTextTerms {
	folded := strings.ToLower(text)

	return VisibleTextTerms{original: text, folded: folded, tokens: foldedTokens(folded)}
}

func NewVisibleURLTerms(rawURL string) VisibleTextTerms {
	return NewVisibleTextTerms(decodedResultURL(rawURL))
}

func (text VisibleTextTerms) Mentions(term string) bool {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return false
	}
	if querymatch.TermContainsWordSeparator(term) {
		_, _, found := querymatch.NextBoundedTerm(text.original, term, 0)

		return found
	}
	if querymatch.TermCanMatchWithinToken(term) {
		return strings.Contains(text.folded, term)
	}

	return anyTokenSharesStem(text.tokens, term)
}
