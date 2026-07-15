package searchindex

import "unicode"

func queryAnalyzerScript(text string) *unicode.RangeTable {
	for _, character := range text {
		if storedCJKCharacter(character) {
			return unicode.Han
		}
	}

	return dominantScript(text)
}
