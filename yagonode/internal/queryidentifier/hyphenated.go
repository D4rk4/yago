package queryidentifier

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type Hyphenated struct {
	First string
	Last  string
}

func ParseHyphenated(text string) Hyphenated {
	text = strings.TrimSpace(text)
	separator := strings.IndexFunc(text, func(r rune) bool { return unicode.Is(unicode.Dash, r) })
	if separator < 0 {
		return Hyphenated{}
	}
	_, width := utf8.DecodeRuneInString(text[separator:])
	first, last := text[:separator], text[separator+width:]
	if strings.TrimSpace(first) != first || strings.TrimSpace(last) != last ||
		!MixedAlphanumeric(first) || !MixedAlphanumeric(last) {
		return Hyphenated{}
	}

	return Hyphenated{First: strings.ToLower(first), Last: strings.ToLower(last)}
}
