// Package contentquality gates crawled text before it is stored and indexed,
// using the deterministic token-based subset of the Gopher (Rae et al.,
// arXiv:2112.11446, Appendix A) and C4 (Raffel et al., arXiv:1910.10683)
// quality rules that FineWeb (arXiv:2406.17557) validated at web scale.
// Deterministic rules keep peers reproducible — no model, no scores, every
// rejection names its rule. Line-based rules are deliberately absent: the
// extractor collapses whitespace, so extracted text has no lines. The
// English-only stopword rule is widened to the node's multilingual function
// words, C4's curly-brace and javascript rules are dropped (a web engine
// indexes technical pages), and unsegmented scripts (CJK, Thai) skip the gate
// — word statistics mean nothing without word boundaries.
package contentquality

import (
	"strings"
	"unicode"

	"github.com/D4rk4/yago/yagonode/internal/stopwords"
)

const (
	minWords              = 50
	maxWords              = 100000
	minMeanWordLength     = 3.0
	maxMeanWordLength     = 10.0
	maxSymbolToWordRatio  = 0.1
	minAlphabeticFraction = 0.8
	minFunctionWords      = 2
	unsegmentedSkipShare  = 0.3
)

// topNGramLimits is the maximum share of characters the single most frequent
// word n-gram may cover (Gopher: 2/3/4-grams at 0.20/0.18/0.16).
var topNGramLimits = map[int]float64{2: 0.20, 3: 0.18, 4: 0.16}

// duplicatedNGramLimits is the maximum share of characters covered by word
// n-grams that occur more than once (Gopher: 5..10-grams at 0.15..0.10).
var duplicatedNGramLimits = map[int]float64{
	5: 0.15, 6: 0.14, 7: 0.13, 8: 0.12, 9: 0.11, 10: 0.10,
}

// RejectionRule names the first quality rule the text violates, or returns ""
// for text worth indexing.
func RejectionRule(text string) string {
	if strings.Contains(strings.ToLower(text), "lorem ipsum") {
		return "lorem-ipsum"
	}
	words := strings.Fields(text)
	if unsegmentedScript(text) {
		return ""
	}
	if rule := wordShapeRule(words); rule != "" {
		return rule
	}
	if rule := repetitionRule(words); rule != "" {
		return rule
	}

	return ""
}

func wordShapeRule(words []string) string {
	if len(words) < minWords {
		return "too-few-words"
	}
	if len(words) > maxWords {
		return "too-many-words"
	}
	runes, alphabetic, symbols, functionWords := 0, 0, 0, 0
	for _, word := range words {
		runes += len([]rune(word))
		if strings.ContainsFunc(word, unicode.IsLetter) {
			alphabetic++
		}
		if strings.Contains(word, "#") || strings.Contains(word, "…") ||
			strings.Contains(word, "...") {
			symbols++
		}
		if stopwords.IsStopword(strings.Trim(word, ".,!?…:;\"'()[]«»—-")) {
			functionWords++
		}
	}
	mean := float64(runes) / float64(len(words))
	if mean < minMeanWordLength || mean > maxMeanWordLength {
		return "word-length"
	}
	if float64(symbols)/float64(len(words)) > maxSymbolToWordRatio {
		return "symbol-ratio"
	}
	if float64(alphabetic)/float64(len(words)) < minAlphabeticFraction {
		return "non-alphabetic"
	}
	if functionWords < minFunctionWords {
		return "no-function-words"
	}

	return ""
}

func repetitionRule(words []string) string {
	for n, limit := range topNGramLimits {
		if topNGramCharacterShare(words, n) > limit {
			return "top-ngram"
		}
	}
	for n, limit := range duplicatedNGramLimits {
		if duplicatedNGramCharacterShare(words, n) > limit {
			return "repeated-ngram"
		}
	}

	return ""
}

// topNGramCharacterShare is the share of the text's word characters covered by
// occurrences of the single most frequent n-gram.
func topNGramCharacterShare(words []string, n int) float64 {
	grams, total := nGramCounts(words, n)
	if total == 0 {
		return 0
	}
	best := 0
	for gram, count := range grams {
		if chars := count * gramCharacters(gram); chars > best {
			best = chars
		}
	}

	return float64(best) / float64(total)
}

// duplicatedNGramCharacterShare is the share of the text's word characters
// covered by n-grams occurring more than once.
func duplicatedNGramCharacterShare(words []string, n int) float64 {
	grams, total := nGramCounts(words, n)
	if total == 0 {
		return 0
	}
	duplicated := 0
	for gram, count := range grams {
		if count > 1 {
			duplicated += count * gramCharacters(gram)
		}
	}

	return float64(duplicated) / float64(total)
}

func nGramCounts(words []string, n int) (map[string]int, int) {
	total := 0
	for _, word := range words {
		total += len([]rune(word))
	}
	if len(words) < n {
		return nil, total
	}
	grams := make(map[string]int, len(words))
	for i := 0; i+n <= len(words); i++ {
		grams[strings.Join(words[i:i+n], " ")]++
	}

	return grams, total
}

func gramCharacters(gram string) int {
	characters := 0
	for _, word := range strings.Fields(gram) {
		characters += len([]rune(word))
	}

	return characters
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
