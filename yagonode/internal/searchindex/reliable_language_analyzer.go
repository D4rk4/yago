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
	if normalizedLanguageCode(code) != "ku" {
		return "", false
	}

	return reliableLanguageAnalyzer(code, text), true
}
