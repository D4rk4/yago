package yagonode

import (
	"context"
	"strings"
	"unicode"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
)

const (
	didYouMeanMaxEditDistance = 2
	didYouMeanMinTermRunes    = 3
	didYouMeanTitleSample     = 10
)

// recoveringSearcher runs the zero-result recovery cascade (YaCy DidYouMean
// parity): when a query with parsed terms finds nothing, it retries once with
// edit-distance-tolerant matching and, when close matches surface, labels the
// response and assembles a "did you mean" spelling suggestion from the words of
// the recovered titles. An honest empty answer stays empty when even the fuzzy
// retry finds nothing.
type recoveringSearcher struct {
	inner     searchcore.Searcher
	corrector func() *spellcheck.Corrector
}

func withZeroResultRecovery(
	inner searchcore.Searcher,
	corrector func() *spellcheck.Corrector,
) searchcore.Searcher {
	return recoveringSearcher{inner: inner, corrector: corrector}
}

func (s recoveringSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	resp, err := s.inner.Search(ctx, req)
	if err != nil || len(resp.Results) > 0 || req.Fuzzy || len(req.Terms) == 0 {
		//nolint:wrapcheck // pass the wrapped searcher's error through unchanged.
		return resp, err
	}

	retry := req
	retry.Fuzzy = true
	recovered, retryErr := s.inner.Search(ctx, retry)
	if retryErr != nil || len(recovered.Results) == 0 {
		// The recovery retry is best-effort: when it fails or stays empty, the
		// caller gets the primary search's honest empty answer — still carrying a
		// dictionary spelling suggestion when one exists, so a total miss can
		// point at the likely intended query.
		resp.DidYouMean = s.spellSuggestion(req.Terms)

		return resp, nil //nolint:nilerr // deliberate fallback to the empty answer.
	}
	recovered.Request = req
	recovered.Recovered = "fuzzy"
	recovered.DidYouMean = s.didYouMeanFor(req.Terms, recovered.Results)

	return recovered, nil
}

// didYouMeanFor prefers a correction against the whole indexed vocabulary
// (SymSpell) and falls back to the recovered result titles when the dictionary
// offers nothing.
func (s recoveringSearcher) didYouMeanFor(
	terms []string,
	results []searchcore.Result,
) string {
	if suggestion := s.spellSuggestion(terms); suggestion != "" {
		return suggestion
	}

	return didYouMean(terms, results)
}

// spellSuggestion corrects the query terms against the current index-vocabulary
// corrector, returning empty when no corrector is wired or nothing needs fixing.
func (s recoveringSearcher) spellSuggestion(terms []string) string {
	if s.corrector == nil {
		return ""
	}
	corrector := s.corrector()
	if corrector == nil {
		return ""
	}

	return corrector.CorrectQuery(terms)
}

// didYouMean rebuilds the query with each term replaced by the closest word
// (edit distance 1..2) found in the recovered result titles; it returns empty
// when no term improves, so surfaces only suggest genuinely different spellings.
func didYouMean(terms []string, results []searchcore.Result) string {
	vocabulary := titleWords(results)
	corrected := make([]string, 0, len(terms))
	changed := false
	for _, term := range terms {
		replacement := closestWord(strings.ToLower(term), vocabulary)
		if replacement == "" {
			corrected = append(corrected, term)

			continue
		}
		corrected = append(corrected, replacement)
		changed = true
	}
	if !changed {
		return ""
	}

	return strings.Join(corrected, " ")
}

// titleWords collects lowercase words from the first recovered titles as the
// correction vocabulary.
func titleWords(results []searchcore.Result) []string {
	words := make([]string, 0, didYouMeanTitleSample*4)
	for i, result := range results {
		if i >= didYouMeanTitleSample {
			break
		}
		fields := strings.FieldsFunc(strings.ToLower(result.Title), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
		words = append(words, fields...)
	}

	return words
}

// closestWord picks the vocabulary word nearest to term within the allowed edit
// distance; identical words return empty because they need no correction.
func closestWord(term string, vocabulary []string) string {
	if len([]rune(term)) < didYouMeanMinTermRunes {
		return ""
	}
	best, bestDistance := "", didYouMeanMaxEditDistance+1
	for _, word := range vocabulary {
		if len([]rune(word)) < didYouMeanMinTermRunes || word == term {
			continue
		}
		if distance := editDistance(term, word); distance < bestDistance {
			best, bestDistance = word, distance
		}
	}

	return best
}

// editDistance is the Levenshtein distance over runes.
func editDistance(a, b string) int {
	left, right := []rune(a), []rune(b)
	previous := make([]int, len(right)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(left); i++ {
		current := make([]int, len(right)+1)
		current[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			current[j] = min(current[j-1]+1, min(previous[j]+1, previous[j-1]+cost))
		}
		previous = current
	}

	return previous[len(right)]
}
