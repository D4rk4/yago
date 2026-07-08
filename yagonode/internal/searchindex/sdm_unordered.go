package searchindex

import "strings"

// sdmUnorderedWindow is the token span within which two query words count as an
// unordered co-occurrence for the Sequential Dependence Model unordered-window
// feature. It matches the near-filter window: close enough that the words plausibly
// belong to one phrase or clause rather than merely sharing the document.
const sdmUnorderedWindow = 8

// unorderedProximity is the SDM unordered-window feature (Metzler & Croft, SIGIR
// 2005) that the ordered bigram boost leaves out because bleve has no unordered
// operator: the fraction of adjacent query-word pairs whose two words co-occur
// within sdmUnorderedWindow tokens of each other in the text, order-independent.
// It is computed by a body scan at result mapping and folded into the score as a
// learned ranking weight; the ordered feature, which bleve can express, rides the
// query instead. It is 0 when the query carries fewer than two distinct words,
// where there is no pair to measure, and 0 for text that matches no pair.
func unorderedProximity(text string, terms []string) float64 {
	words := distinctWords(terms)
	if len(words) < 2 {
		return 0
	}
	positions := wordPositions(text, words)
	satisfied := 0
	for i := 0; i+1 < len(words); i++ {
		if withinWindow(positions[words[i]], positions[words[i+1]], sdmUnorderedWindow) {
			satisfied++
		}
	}

	return float64(satisfied) / float64(len(words)-1)
}

// distinctWords lowercases and trims the query terms, dropping blanks and
// duplicates while preserving order, so the adjacent-pair walk mirrors the ordered
// bigram feature's while never pairing a repeated word with itself.
func distinctWords(terms []string) []string {
	seen := make(map[string]bool, len(terms))
	words := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		words = append(words, term)
	}

	return words
}

// wordPositions records the ascending token positions of each wanted word in the
// text in a single pass over the same tokenizer the near filter uses.
func wordPositions(text string, words []string) map[string][]int {
	wanted := make(map[string]bool, len(words))
	for _, word := range words {
		wanted[word] = true
	}
	positions := make(map[string][]int, len(words))
	for index, token := range textTokens(text) {
		if wanted[token] {
			positions[token] = append(positions[token], index)
		}
	}

	return positions
}

// withinWindow reports whether any position in left and any in right lie within
// window tokens of each other; both slices are ascending, so advancing the smaller
// cursor finds the closest pair in linear time.
func withinWindow(left, right []int, window int) bool {
	i, j := 0, 0
	for i < len(left) && j < len(right) {
		diff := left[i] - right[j]
		if diff < 0 {
			diff = -diff
		}
		if diff <= window {
			return true
		}
		if left[i] < right[j] {
			i++
		} else {
			j++
		}
	}

	return false
}
