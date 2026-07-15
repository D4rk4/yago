package searchcore

import (
	"strings"
	"unicode/utf8"
)

func TokenMatchesTerm(token string, term string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	term = strings.ToLower(strings.TrimSpace(term))
	if token == "" || term == "" {
		return false
	}
	if token == term {
		return true
	}
	needed := max(
		mentionMinPrefixRunes,
		utf8.RuneCountInString(term)-mentionSuffixSlackRunes,
	)

	return tokenSharedPrefixRunes(token, term) >= needed
}

func tokenSharedPrefixRunes(left string, right string) int {
	shared := 0
	for left != "" && right != "" {
		leftRune, leftWidth := utf8.DecodeRuneInString(left)
		rightRune, rightWidth := utf8.DecodeRuneInString(right)
		if leftRune != rightRune {
			break
		}
		shared++
		left = left[leftWidth:]
		right = right[rightWidth:]
	}

	return shared
}
