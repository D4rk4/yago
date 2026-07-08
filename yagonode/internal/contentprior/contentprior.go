// Package contentprior scores indexed text on a deterministic, language-agnostic
// content-quality prior in [0,1] for the YagoRank ranking model (ADR-0035). It
// reuses the Bendersky (WSDM 2011) / FineWeb (arXiv:2406.17557) feature family the
// crawl-time contentquality gate uses, but grades quality on a continuous scale
// instead of accepting or rejecting: a document that cleared the hard gate can
// still be keyword-stuffed (a handful of function words in hundreds) or
// symbol-heavy, and the graded prior demotes it below clean prose. Text that
// cannot be measured — too short to be significant, or an unsegmented script (CJK,
// Thai) where space-token statistics are meaningless — scores the neutral 1.0 so
// it is neither rewarded nor punished.
package contentprior

import (
	"strings"
	"unicode"

	"github.com/D4rk4/yago/yagonode/internal/stopwords"
)

const (
	// minScoredWords is the token floor below which word statistics are too noisy
	// to grade; shorter text takes the neutral score.
	minScoredWords = 20
	// idealFunctionFraction is the share of function (stop) words at which the
	// function-word feature saturates; real prose sits at or above it, while
	// keyword-stuffed text sits far below.
	idealFunctionFraction = 0.20
	// maxSymbolFraction is the share of hash/ellipsis symbol words at which the
	// symbol feature reaches zero.
	maxSymbolFraction = 0.10
	// idealAlphabeticFraction is the share of alphabetic words at which the
	// alphabetic feature saturates.
	idealAlphabeticFraction = 0.85
	// unsegmentedSkipShare is the share of unsegmented-script letters above which
	// space-token statistics are meaningless and the text takes the neutral score.
	unsegmentedSkipShare = 0.30
	neutralScore         = 1.0
)

var wordTrimCutset = ".,!?…:;\"'()[]«»—-"

// Score grades text quality in [0,1]: clean prose with a healthy share of function
// words scores near 1, keyword-stuffed or symbol-heavy text near 0. It is a pure
// function of the text — deterministic, so peers agree — and O(n) in the token
// count.
func Score(text string) float64 {
	words := strings.Fields(text)
	if len(words) < minScoredWords || unsegmentedScript(text) {
		return neutralScore
	}
	functionWords, symbolWords, alphabeticWords := 0, 0, 0
	for _, word := range words {
		if stopwords.IsStopword(strings.Trim(word, wordTrimCutset)) {
			functionWords++
		}
		if hasSymbol(word) {
			symbolWords++
		}
		if strings.ContainsFunc(word, unicode.IsLetter) {
			alphabeticWords++
		}
	}
	total := float64(len(words))
	functionScore := clamp01(float64(functionWords) / total / idealFunctionFraction)
	symbolScore := 1 - clamp01(float64(symbolWords)/total/maxSymbolFraction)
	alphabeticScore := clamp01(float64(alphabeticWords) / total / idealAlphabeticFraction)

	// The function-word fraction is the dominant prose-vs-spam signal (Bendersky
	// WSDM 2011), so it multiplies; symbol and alphabetic cleanliness only
	// modulate. Keyword-stuffed text (near-zero function words) collapses to ~0.
	return functionScore * (symbolScore + alphabeticScore) / 2
}

func hasSymbol(word string) bool {
	return strings.Contains(word, "#") ||
		strings.Contains(word, "…") ||
		strings.Contains(word, "...")
}

func clamp01(value float64) float64 {
	return min(1, max(0, value))
}

// unsegmentedScript reports whether a meaningful share of the text's letters
// belongs to scripts written without word separators, where space-token
// statistics are meaningless.
func unsegmentedScript(text string) bool {
	letters, unsegmented := 0, 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana,
			unicode.Hangul, unicode.Thai, unicode.Lao, unicode.Khmer, unicode.Myanmar) {
			unsegmented++
		}
	}

	return letters > 0 && float64(unsegmented)/float64(letters) > unsegmentedSkipShare
}
