package searchcore

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// mentionMinPrefixRunes is the shortest shared token prefix accepted as an
	// inflection of a query term, matching the stem-length floor the word-forms
	// expander uses.
	mentionMinPrefixRunes = 4
	// mentionSuffixSlackRunes is how many trailing runes of a query term may
	// differ from a token that shares its prefix, tolerating case endings
	// («черногория»/«черногории») without language-specific suffix rules.
	mentionSuffixSlackRunes = 2
)

// ResultMentionsTerms reports whether any query term is evident in the result's
// own metadata — title, snippet, or URL. A term counts when the folded text
// contains it verbatim (which also serves scripts without word boundaries) or
// when a token shares a stem-length prefix with it, so an inflected surface
// form still verifies. An empty term list has nothing to check and passes.
func ResultMentionsTerms(result Result, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	haystack := strings.ToLower(
		result.Title + " " + result.Snippet + " " + decodedResultURL(result.URL),
	)
	tokens := foldedTokens(haystack)
	for _, term := range terms {
		if foldedTextMentionsTerm(haystack, tokens, term) {
			return true
		}
	}

	return false
}

// TextMentionsTerm reports whether the folded text contains the term verbatim
// or a token sharing a stem-length prefix with it — the same evidence rule
// result verification uses, exposed for callers holding a fetched document
// body rather than a Result.
func TextMentionsTerm(text string, term string) bool {
	folded := strings.ToLower(text)

	return foldedTextMentionsTerm(folded, foldedTokens(folded), term)
}

func foldedTokens(folded string) []string {
	return strings.FieldsFunc(folded, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func foldedTextMentionsTerm(folded string, tokens []string, term string) bool {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return false
	}

	return strings.Contains(folded, term) || anyTokenSharesStem(tokens, term)
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

// anyTokenSharesStem reports whether any token is a plausible inflection of the
// term: both must run at least mentionMinPrefixRunes together from the start,
// and the shared prefix must reach all but mentionSuffixSlackRunes of the term.
func anyTokenSharesStem(tokens []string, term string) bool {
	termRunes := utf8.RuneCountInString(term)
	needed := max(mentionMinPrefixRunes, termRunes-mentionSuffixSlackRunes)
	for _, token := range tokens {
		if sharedPrefixRunes(token, term) >= needed {
			return true
		}
	}

	return false
}

// sharedPrefixRunes counts the leading runes two words have in common.
func sharedPrefixRunes(a, b string) int {
	runesA := []rune(a)
	runesB := []rune(b)
	limit := min(len(runesA), len(runesB))
	shared := 0
	for shared < limit && runesA[shared] == runesB[shared] {
		shared++
	}

	return shared
}
