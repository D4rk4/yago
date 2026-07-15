package queryidentifier

import (
	"strings"
	"unicode"
)

func MixedAlphanumeric(term string) bool {
	letters := false
	numbers := false
	for _, character := range strings.TrimSpace(term) {
		switch {
		case unicode.IsLetter(character):
			letters = true
		case unicode.IsNumber(character):
			numbers = true
		case unicode.IsMark(character):
		default:
			return false
		}
	}

	return letters && numbers
}
