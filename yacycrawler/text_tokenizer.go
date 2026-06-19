package yacycrawler

import (
	"strings"
	"unicode"
)

const MinWordLength = 2

func Tokenize(text string) []string {
	tokens := make([]string, 0)
	var word strings.Builder
	flush := func() {
		if word.Len() >= MinWordLength {
			tokens = append(tokens, word.String())
		}
		word.Reset()
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func NormalizeLanguage(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if len(lang) >= 2 {
		return lang[:2]
	}
	return "en"
}
