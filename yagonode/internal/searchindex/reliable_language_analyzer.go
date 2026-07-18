package searchindex

import (
	"strings"
	"unicode"
)

func normalizedLanguageCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if index := strings.IndexAny(code, "-_"); index > 0 {
		return code[:index]
	}

	return code
}

func reliableLanguageAnalyzer(code string, text string) string {
	analyzer := languageToAnalyzer(code)
	if analyzer == standardTextAnalyzer && normalizedLanguageCode(code) == "ku" &&
		dominantScript(text) == unicode.Arabic {
		return "ckb"
	}

	return analyzer
}

func scriptQualifiedLanguageAnalyzer(code string, text string) (string, bool) {
	switch normalizedLanguageCode(code) {
	case "zh":
		return cjkChineseTextAnalyzer, containsScript(text, unicode.Han)
	case "ja":
		return cjkJapaneseTextAnalyzer, containsAnyScript(
			text,
			unicode.Han,
			unicode.Hiragana,
			unicode.Katakana,
		)
	case "ko":
		return cjkKoreanTextAnalyzer, containsScript(text, unicode.Hangul)
	case "ku":
		return reliableLanguageAnalyzer(code, text), true
	default:
		return "", false
	}
}

func containsScript(text string, script *unicode.RangeTable) bool {
	return containsAnyScript(text, script)
}

func containsAnyScript(text string, scripts ...*unicode.RangeTable) bool {
	for _, character := range text {
		for _, script := range scripts {
			if unicode.Is(script, character) {
				return true
			}
		}
	}

	return false
}
