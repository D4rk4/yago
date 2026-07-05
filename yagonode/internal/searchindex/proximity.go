package searchindex

import (
	"strings"
	"unicode"
)

// nearTokenWindow is how many consecutive text tokens a "near" query allows
// between its terms: every term must appear inside one window this wide.
const nearTokenWindow = 8

// termsNear reports whether every term occurs within one nearTokenWindow-sized
// run of tokens in text. Bleve's match-phrase query carries no slop in this
// version, so proximity is enforced as a post-retrieval filter over the stored
// text — the documents are already loaded for the other request filters.
func termsNear(text string, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	wanted := make(map[string]int, len(terms))
	for _, term := range terms {
		wanted[strings.ToLower(term)]++
	}
	tokens := textTokens(text)

	matched := 0
	inWindow := map[string]int{}
	for i, token := range tokens {
		if i >= nearTokenWindow {
			if left := tokens[i-nearTokenWindow]; wanted[left] > 0 {
				inWindow[left]--
				if inWindow[left] < wanted[left] {
					matched--
				}
			}
		}
		if wanted[token] > 0 {
			inWindow[token]++
			if inWindow[token] <= wanted[token] {
				matched++
			}
		}
		if matched == totalWanted(wanted) {
			return true
		}
	}

	return false
}

func totalWanted(wanted map[string]int) int {
	total := 0
	for _, count := range wanted {
		total += count
	}

	return total
}

func textTokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
