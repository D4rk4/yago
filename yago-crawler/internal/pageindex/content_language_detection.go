package pageindex

import (
	"unicode/utf8"

	"github.com/RadhiFadlillah/whatlanggo"
)

const maximumLanguageEvidenceBytes = 64 << 10

func resolveContentLanguage(text, hint string) string {
	info := whatlanggo.Detect(languageEvidence(text))
	detected, detectedAvailable := languageCode(info.Lang.Iso6391())
	if detectedAvailable && info.IsReliable() {
		return detected
	}
	if declared, declaredAvailable := languageCode(hint); declaredAvailable {
		return declared
	}
	if detectedAvailable {
		return detected
	}
	return "en"
}

func languageEvidence(text string) string {
	if len(text) <= maximumLanguageEvidenceBytes {
		return text
	}
	end := maximumLanguageEvidenceBytes
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end]
}
