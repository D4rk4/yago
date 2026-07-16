package searchcore

import (
	"net/url"
	"strings"
	"unicode"

	"github.com/D4rk4/yago/yagonode/internal/querymatch"
)

func ResultMentionsTerms(result Result, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	visible := NewVisibleTextTerms(
		result.Title + " " + result.Snippet + " " + decodedResultURL(result.URL),
	)
	for _, term := range terms {
		if visible.Mentions(term) {
			return true
		}
	}

	return false
}

func foldedTokens(folded string) []string {
	return strings.FieldsFunc(folded, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r)
	})
}

// decodedResultURL folds percent-encoding out of a result URL so query words
// hidden in encoded paths still count as mentions; an undecodable URL is used
// as sent.
func decodedResultURL(rawURL string) string {
	decoded, err := url.QueryUnescape(rawURL)
	if err != nil {
		return rawURL
	}

	return decoded
}

func anyTokenSharesStem(tokens []string, term string) bool {
	for _, token := range tokens {
		if querymatch.TokenMatchesTerm(token, term) {
			return true
		}
	}

	return false
}
