package querymatch

import (
	"strings"
	"unicode"
)

func TermCanMatchWithinToken(term string) bool {
	for _, current := range strings.TrimSpace(term) {
		if unicode.In(
			current,
			unicode.Bopomofo,
			unicode.Han,
			unicode.Hangul,
			unicode.Hiragana,
			unicode.Katakana,
			unicode.Khmer,
			unicode.Lao,
			unicode.Myanmar,
			unicode.Thai,
			unicode.Tibetan,
		) {
			return true
		}
	}

	return false
}
