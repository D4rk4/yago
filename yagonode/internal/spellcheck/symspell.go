// Package spellcheck corrects query spelling against the node's own indexed
// vocabulary with the Symmetric Delete algorithm (SymSpell, Garbe 2012): the
// dictionary is precomputed once so a lookup only deletes characters from the
// query term, never the dictionary, making correction fast enough to run on
// every zero-result query.
package spellcheck

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// defaultMaxEditDistance is the largest edit distance a correction may span,
	// matching the zero-result recovery's fuzzy tolerance.
	defaultMaxEditDistance = 2
	// defaultMinTermLen skips short tokens where a two-edit correction would be
	// more guess than fix.
	defaultMinTermLen = 4
	defaultMaxTermLen = 32
)

// Corrector suggests spelling corrections from a fixed term-frequency
// dictionary. The zero value corrects nothing, so an unbuilt corrector is safe
// to consult.
type Corrector struct {
	frequency        map[string]int
	vocabulary       []string
	deleteIndex      map[string][]int
	deleteReferences int
	deleteBytes      int
	maxEditDistance  int
}

// New builds a corrector from a term→frequency dictionary. Terms are lowercased;
// frequency breaks ties so the more common spelling wins.
func New(frequency map[string]int) *Corrector {
	return newCorrector(frequency, correctorLimits{
		vocabularyTerms:  maximumVocabularyTerms,
		deleteReferences: maximumDeleteReferences,
		deleteBytes:      maximumDeleteIndexBytes,
	})
}

// Suggest returns the best correction for one term and whether it differs from
// the input. A term already in the dictionary, too short to correct, or with no
// close dictionary word returns the input unchanged.
func (c *Corrector) Suggest(term string) (string, bool) {
	term = strings.ToLower(strings.TrimSpace(term))
	if c == nil || len(c.frequency) == 0 || !correctableTerm(term) {
		return term, false
	}
	if _, ok := c.frequency[term]; ok {
		return term, false
	}

	candidates := map[int]struct{}{}
	for variant := range deleteVariants(term, c.maxEditDistance) {
		for _, identifier := range c.deleteIndex[variant] {
			candidates[identifier] = struct{}{}
		}
	}

	best := candidate{distance: c.maxEditDistance + 1}
	for identifier := range candidates {
		word := c.vocabulary[identifier]
		current := candidate{
			term:      word,
			distance:  editDistance(term, word),
			frequency: c.frequency[word],
		}
		if current.distance <= c.maxEditDistance && current.betterThan(best) {
			best = current
		}
	}
	if best.term == "" || best.term == term {
		return term, false
	}

	return best.term, true
}

// CorrectQuery corrects each term and returns the rebuilt query when at least
// one term changed, or empty when the query needs no correction — so a surface
// only offers a "did you mean" for a genuinely different spelling.
func (c *Corrector) CorrectQuery(terms []string) string {
	corrected := make([]string, 0, len(terms))
	changed := false
	for _, term := range terms {
		suggestion, ok := c.Suggest(term)
		if ok {
			changed = true
		}
		corrected = append(corrected, suggestion)
	}
	if !changed {
		return ""
	}

	return strings.Join(corrected, " ")
}

// candidate is one correction under consideration during a lookup.
type candidate struct {
	term      string
	distance  int
	frequency int
}

// betterThan ranks candidates by ascending edit distance, then descending
// frequency, then the term itself for determinism; any real candidate beats the
// empty sentinel.
func (c candidate) betterThan(other candidate) bool {
	if other.term == "" {
		return true
	}
	if c.distance != other.distance {
		return c.distance < other.distance
	}
	if c.frequency != other.frequency {
		return c.frequency > other.frequency
	}

	return c.term < other.term
}

// deleteVariants returns the input plus every string reachable by deleting up to
// maxEdits runes, the key set SymSpell matches a query term against.
func deleteVariants(term string, maxEdits int) map[string]bool {
	variants := map[string]bool{term: true}
	frontier := []string{term}
	for edit := 0; edit < maxEdits; edit++ {
		next := make([]string, 0)
		for _, current := range frontier {
			runes := []rune(current)
			if len(runes) <= 1 {
				continue
			}
			for i := range runes {
				variant := string(runes[:i]) + string(runes[i+1:])
				if !variants[variant] {
					variants[variant] = true
					next = append(next, variant)
				}
			}
		}
		frontier = next
	}

	return variants
}

// editDistance is the Levenshtein distance between two rune strings.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	previous := make([]int, len(rb)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		current := make([]int, len(rb)+1)
		current[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			current[j] = minInt(
				previous[j]+1,
				current[j-1]+1,
				previous[j-1]+cost,
			)
		}
		previous = current
	}

	return previous[len(rb)]
}

func minInt(values ...int) int {
	best := values[0]
	for _, value := range values[1:] {
		if value < best {
			best = value
		}
	}

	return best
}

// TermFrequencies accumulates a term→count dictionary from free text into dst,
// the input to New; callers pass the same map across documents. Tokens shorter
// than the minimum correctable length are skipped so the dictionary stays
// relevant to what the corrector can act on.
func TermFrequencies(dst map[string]int, text string) {
	termsInText(text, func(term string) { dst[term]++ })
}

func termsInText(text string, visit func(string)) {
	for term := range strings.FieldsFuncSeq(text, isNotWordRune) {
		if correctableTerm(term) {
			visit(strings.ToLower(term))
		}
	}
}

func correctableTerm(term string) bool {
	length := utf8.RuneCountInString(term)

	return length >= defaultMinTermLen && length <= defaultMaxTermLen
}

// isNotWordRune splits on any non-letter, non-digit rune so punctuation and
// markup around a word do not fuse into the token.
func isNotWordRune(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
}
