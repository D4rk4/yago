package pageparse

import (
	"strings"
	"unicode"
)

const MinWordLength = 2

func Tokenize(text string) []string {
	return tokenize(text, 0)
}

func tokenize(text string, maximum int) []string {
	raw := splitWords([]rune(text), maximum)
	tokens := make([]string, 0, len(raw))
	for _, w := range raw {
		lower := strings.ToLower(w)
		tokens = append(tokens, lower)
		if maximum > 0 && len(tokens) == maximum {
			break
		}
	}
	return tokens
}

//nolint:gocognit,revive // FIXME: split token classification transitions after the new lint rules are committed.
func splitWords(r []rune, maximum int) []string {
	out := make([]string, 0)
	var sb []rune
	wasDigitSep := false
	flush := func() bool {
		if len(sb) >= MinWordLength {
			out = append(out, string(sb))
		}
		sb = nil

		return maximum > 0 && len(out) == maximum
	}
	n := len(r)
	for i := range n {
		c := r[i]
		if c == '-' && i < n-1 && (isLetter(r[i+1]) || isDigit(r[i+1])) {
			sb = append(sb, c)
			continue
		}
		if isDigitSep(c) && i > 0 && isDigit(r[i-1]) && i < n-1 && isDigit(r[i+1]) {
			sb = append(sb, c)
			wasDigitSep = true
			continue
		}
		if wasDigitSep && isLetter(c) {
			wasDigitSep = false
		}
		switch {
		case isPunctuation(c):
			if len(sb) > 0 && !wasDigitSep {
				if flush() {
					return out
				}
			}
			sb = append(sb, c)
			if flush() {
				return out
			}
			wasDigitSep = false
		case isInvisible(c):
			if flush() {
				return out
			}
			wasDigitSep = false
		default:
			sb = append(sb, c)
			if i < n-1 && isDigit(c) && isLetter(r[i+1]) {
				if flush() {
					return out
				}
			}
		}
	}
	flush()
	return out
}

func isLetter(c rune) bool { return unicode.IsLetter(c) }

func isDigit(c rune) bool { return unicode.IsDigit(c) }

func isPunctuation(c rune) bool { return c == '.' || c == '!' || c == '?' }

func isDigitSep(c rune) bool { return c == '.' || c == ',' }

func isInvisible(c rune) bool {
	if isLetter(c) || isDigit(c) {
		return false
	}
	return !isPunctuation(c) && !isDigitSep(c)
}
