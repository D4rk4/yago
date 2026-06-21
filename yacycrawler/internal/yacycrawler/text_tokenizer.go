package yacycrawler

import (
	"strings"
	"unicode"
)

const MinWordLength = 2

func Tokenize(text string) []string {
	raw := splitWords([]rune(text))
	tokens := make([]string, 0, len(raw))
	for _, w := range raw {
		lower := strings.ToLower(w)
		if len([]rune(lower)) < MinWordLength {
			continue
		}
		tokens = append(tokens, lower)
	}
	return tokens
}

//nolint:gocognit,revive // FIXME: split token classification transitions after the new lint rules are committed.
func splitWords(r []rune) []string {
	out := make([]string, 0)
	var sb []rune
	wasDigitSep := false
	flush := func() {
		if len(sb) > 0 {
			out = append(out, string(sb))
			sb = nil
		}
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
			flush()
			wasDigitSep = false
		}
		switch {
		case isPunctuation(c):
			if len(sb) > 0 && !wasDigitSep {
				flush()
			}
			sb = append(sb, c)
			flush()
			wasDigitSep = false
		case isInvisible(c):
			flush()
			wasDigitSep = false
		default:
			sb = append(sb, c)
			if i < n-1 && isDigit(c) && isLetter(r[i+1]) {
				flush()
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

func NormalizeLanguage(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if len(lang) >= 2 {
		return lang[:2]
	}
	return "en"
}
