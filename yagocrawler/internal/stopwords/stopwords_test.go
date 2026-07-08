package stopwords_test

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/stopwords"
)

func TestIsStopwordMatchesFoldedFunctionWords(t *testing.T) {
	for _, word := range []string{"the", "The", " и ", "der", "les", "el"} {
		if !stopwords.IsStopword(word) {
			t.Errorf("IsStopword(%q) = false, want true", word)
		}
	}
	for _, word := range []string{"kangaroo", "кенгуру", ""} {
		if stopwords.IsStopword(word) {
			t.Errorf("IsStopword(%q) = true, want false", word)
		}
	}
}
