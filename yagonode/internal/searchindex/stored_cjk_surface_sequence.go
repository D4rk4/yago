package searchindex

import "unicode"

func storedCJKCharacter(character rune) bool {
	return unicode.In(
		character,
		unicode.Han,
		unicode.Hiragana,
		unicode.Katakana,
		unicode.Hangul,
	)
}

func (f *storedFieldEvidence) addCJKSurfaceTerm(
	value storedCJKValue,
	start int,
	end int,
	position uint64,
) uint64 {
	position++
	f.addCJKMatches(
		value.matcher,
		normalizedUnstemmedWord(value.text[start:end], "cjk"),
		value.text[start:end],
		storedLocationCoordinates{
			position:    position,
			start:       start,
			end:         end,
			arrayIndex:  value.arrayIndex,
			arrayLength: value.arrayLength,
		},
	)

	return position
}
