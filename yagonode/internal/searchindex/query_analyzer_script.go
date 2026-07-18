package searchindex

import "unicode"

func queryAnalyzerScript(text string) *unicode.RangeTable {
	hasHan := false
	for _, character := range text {
		switch {
		case unicode.Is(unicode.Hiragana, character):
			return unicode.Hiragana
		case unicode.Is(unicode.Katakana, character):
			return unicode.Katakana
		case unicode.Is(unicode.Hangul, character):
			return unicode.Hangul
		case unicode.Is(unicode.Han, character):
			hasHan = true
		}
	}
	if hasHan {
		return unicode.Han
	}

	return dominantScript(text)
}
