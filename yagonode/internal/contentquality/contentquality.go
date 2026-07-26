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
	// loremRunWords is how many consecutive placeholder words mark filler rather
	// than a page discussing the placeholder.
	loremRunWords = 8
	// wordTrimCutset strips the punctuation that clings to a word token.
	wordTrimCutset = ".,!?\u2026:;\"'()[]\u00ab\u00bb\u2014-"
)

// loremWords are the classic lorem ipsum tokens; a long unbroken run of them is
// filler, an isolated mention is a page about typography.
var loremWords = map[string]bool{
	"lorem": true, "ipsum": true, "dolor": true, "sit": true, "amet": true,
	"consectetur": true, "adipiscing": true, "elit": true, "sed": true,
	"do": true, "eiusmod": true, "tempor": true, "incididunt": true,
	"ut": true, "labore": true, "et": true, "dolore": true, "magna": true,
	"aliqua": true, "enim": true, "ad": true, "minim": true, "veniam": true,
	"quis": true, "nostrud": true, "exercitation": true, "ullamco": true,
	"laboris": true, "nisi": true, "aliquip": true, "ex": true, "ea": true,
	"commodo": true, "consequat": true, "duis": true, "aute": true,
	"irure": true, "in": true, "reprehenderit": true, "voluptate": true,
	"velit": true, "esse": true, "cillum": true, "eu": true, "fugiat": true,
	"nulla": true, "pariatur": true,
}

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
	words := strings.Fields(text)
	if placeholderText(words) {
		return "lorem-ipsum"
	}
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
		if stopwords.IsStopword(strings.Trim(word, wordTrimCutset)) {
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
	// This rule over-rejects prose in languages the stopword dictionary does not
	// cover. Skipping the rule when no dictionary word appears is not the fix:
	// genuine keyword stuffing has no function words either, so the two are
	// indistinguishable without language detection. Left as is deliberately.
	if functionWords < minFunctionWords {
		return "no-function-words"
	}

	return ""
}

// placeholderText reports whether filler text makes up a meaningful share of the
// page. Matching the phrase anywhere discarded every page that merely discusses
// it — typography guides, CSS documentation, generators, the encyclopedia entry.
func placeholderText(words []string) bool {
	if len(words) < loremRunWords {
		return false
	}
	run := 0
	for _, word := range words {
		if loremWords[strings.ToLower(strings.Trim(word, wordTrimCutset))] {
			run++
			if run >= loremRunWords {
				return true
			}

			continue
		}
		run = 0
	}

	return false
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
// the most frequent word n-gram (Gopher Table A1). Occurrences of one gram can
// overlap ("a a a a"), so the words they cover are marked and each character is
// counted once — the result is a true fraction of the text. A gram seen only
// once is not repetition, so text with no repeated n-gram scores zero.
func topNGramCharacterShare(words []string, n int) float64 {
	grams, counts, total := nGramPositions(words, n)
	if total == 0 || len(grams) == 0 {
		return 0
	}
	top, topCount, topChars := "", 0, 0
	for gram, count := range counts {
		if count < 2 || count < topCount {
			continue
		}
		chars := gramCharacters(gram)
		if count == topCount &&
			(chars < topChars || (chars == topChars && gram >= top)) {
			continue
		}
		top, topCount, topChars = gram, count, chars
	}
	if top == "" {
		return 0
	}
	covered := make([]bool, len(words))
	for i, gram := range grams {
		if gram == top {
			markCovered(covered, i, n)
		}
	}

	return float64(coveredCharacters(words, covered)) / float64(total)
}

// duplicatedNGramCharacterShare is the share of the text's word characters that
// are redundant: inside a repeat of an n-gram already seen earlier in the same
// text (Gopher Table A1, as implemented by the FineWeb reference pipeline). The
// first occurrence of a phrase is the page saying something; only its repeats
// are duplication. A matched repeat advances the scan by n words, so a
// character sitting inside many overlapping duplicate n-grams is still counted
// once and the result stays a fraction.
//
// Counting every occurrence of every duplicated gram, over a window that slides
// by one word, made this value exceed 1 on ordinary prose — a documentation
// page measured 4.075 against a 0.15 limit — so the gate rejected encyclopedia
// articles and software manuals as repetition spam.
func duplicatedNGramCharacterShare(words []string, n int) float64 {
	total := characterCount(words)
	if total == 0 || n <= 0 || len(words) < n {
		return 0
	}
	seen := make(map[string]struct{}, len(words))
	covered := make([]bool, len(words))
	for i := 0; i+n <= len(words); {
		gram := strings.Join(words[i:i+n], " ")
		if _, duplicate := seen[gram]; !duplicate {
			seen[gram] = struct{}{}
			i++

			continue
		}
		markCovered(covered, i, n)
		i += n
	}

	return float64(coveredCharacters(words, covered)) / float64(total)
}

// nGramPositions lists the text's overlapping word n-grams in order — grams[i]
// starts at word i — with each distinct gram's occurrence count and the text's
// total word-character count.
func nGramPositions(words []string, n int) ([]string, map[string]int, int) {
	total := characterCount(words)
	if n <= 0 || len(words) < n {
		return nil, nil, total
	}
	grams := make([]string, 0, len(words)-n+1)
	counts := make(map[string]int, len(words)-n+1)
	for i := 0; i+n <= len(words); i++ {
		gram := strings.Join(words[i:i+n], " ")
		grams = append(grams, gram)
		counts[gram]++
	}

	return grams, counts, total
}

func markCovered(covered []bool, start, n int) {
	for i := start; i < start+n && i < len(covered); i++ {
		covered[i] = true
	}
}

// coveredCharacters counts the characters of the marked words, each once.
func coveredCharacters(words []string, covered []bool) int {
	characters := 0
	for i, word := range words {
		if covered[i] {
			characters += len([]rune(word))
		}
	}

	return characters
}

func characterCount(words []string) int {
	characters := 0
	for _, word := range words {
		characters += len([]rune(word))
	}

	return characters
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
		// Hangul is deliberately absent: Korean separates words with spaces, so
		// its token statistics are meaningful and it must face the gate like any
		// other segmented script. Listing it here let every Korean page, keyword
		// spam included, skip every rule.
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana,
			unicode.Thai, unicode.Lao, unicode.Khmer, unicode.Myanmar) {
			unsegmented++
		}
	}

	return letters > 0 && float64(unsegmented)/float64(letters) > unsegmentedSkipShare
}
