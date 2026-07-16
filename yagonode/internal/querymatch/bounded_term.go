package querymatch

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func NextBoundedTerm(text string, term string, from int) (int, int, bool) {
	for offset := from; offset <= len(text); {
		start, end, found := NextLiteralTerm(text, term, offset)
		if !found {
			return 0, 0, false
		}
		if startsBoundedTerm(text, start) && endsBoundedTerm(text, end) {
			return start, end, true
		}
		_, width := utf8.DecodeRuneInString(text[start:])
		offset = start + width
	}

	return 0, 0, false
}

func NextLiteralTerm(text string, term string, from int) (int, int, bool) {
	term = strings.TrimSpace(term)
	if text == "" || term == "" || from < 0 || from > len(text) {
		return 0, 0, false
	}
	termRunes := utf8.RuneCountInString(term)
	for start := from; start < len(text); {
		end, complete := termWindowEnd(text, start, termRunes)
		if complete && strings.EqualFold(text[start:end], term) {
			return start, end, true
		}
		_, width := utf8.DecodeRuneInString(text[start:])
		start += width
	}

	return 0, 0, false
}

func termWindowEnd(text string, start int, runes int) (int, bool) {
	end := start
	for range runes {
		if end >= len(text) {
			return 0, false
		}
		_, width := utf8.DecodeRuneInString(text[end:])
		end += width
	}

	return end, true
}

func TermContainsWordSeparator(term string) bool {
	for _, current := range strings.TrimSpace(term) {
		if !termWordRune(current) {
			return true
		}
	}

	return false
}

func startsBoundedTerm(text string, start int) bool {
	if start == 0 {
		return true
	}
	previous, _ := utf8.DecodeLastRuneInString(text[:start])

	return !termWordRune(previous)
}

func endsBoundedTerm(text string, end int) bool {
	if end == len(text) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(text[end:])

	return !termWordRune(next)
}

func termWordRune(current rune) bool {
	return unicode.IsLetter(current) || unicode.IsNumber(current) || unicode.IsMark(current)
}
