package searchcore

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const (
	// prfFeedbackDocs is how many top results are treated as pseudo-relevant when
	// mining expansion terms (Lavrenko & Croft RM3, SIGIR 2001).
	prfFeedbackDocs = 5
	// prfExpansionTerms caps the terms appended to the query, keeping expansion
	// conservative so the added recall does not drift off topic.
	prfExpansionTerms = 3
	// prfMinFeedbackDocFreq requires an expansion term to appear in at least this
	// many feedback documents, favoring terms central to the pseudo-relevant set
	// over per-document noise.
	prfMinFeedbackDocFreq = 2
	// prfMinTermLen drops short tokens that carry little topical signal.
	prfMinTermLen = 4
	// prfActivateBelow gates expansion to recall-poor queries: when the first
	// pass already returns this many results there is enough to rank and
	// expansion would only risk drift.
	prfActivateBelow = 50
)

// NewPseudoRelevanceSearcher expands a recall-poor query with terms mined from
// its own top results (pseudo-relevance feedback, RM3) and fuses the expanded
// pass with the original by reciprocal rank, so the added terms widen recall and
// lift on-topic documents without displacing the original matches. It wraps the
// local searcher: peers run their own retrieval, and a full first page skips
// expansion entirely.
func NewPseudoRelevanceSearcher(inner Searcher) Searcher {
	return pseudoRelevanceSearcher{inner: inner}
}

type pseudoRelevanceSearcher struct {
	inner Searcher
}

func (s pseudoRelevanceSearcher) Search(
	ctx context.Context,
	req Request,
) (Response, error) {
	first, err := s.inner.Search(ctx, req)
	if err != nil {
		return Response{}, fmt.Errorf("pseudo-relevance first pass: %w", err)
	}
	if len(first.Results) == 0 || len(first.Results) >= prfActivateBelow {
		return first, nil
	}
	expansion := minePseudoRelevanceTerms(first.Results, rerankQueryTerms(req))
	if len(expansion) == 0 {
		return first, nil
	}

	expanded := req
	expanded.Query = strings.TrimSpace(req.Query + " " + strings.Join(expansion, " "))
	expanded.Terms = append(append([]string(nil), req.Terms...), expansion...)
	second, err := s.inner.Search(ctx, expanded)
	if err != nil {
		// Expansion is best-effort: a failed second pass keeps the first result.
		return first, nil //nolint:nilerr // the original answer stands.
	}

	first.Results = FuseByReciprocalRank(first.Results, second.Results)

	return first, nil
}

// minePseudoRelevanceTerms selects the most topically central expansion terms
// from the feedback documents' titles and snippets: tokens absent from the query
// and stoplist, at least prfMinTermLen long, appearing in at least
// prfMinFeedbackDocFreq feedback documents, ranked by document then total
// frequency (ties broken by the term for determinism).
func minePseudoRelevanceTerms(results []Result, queryTerms []string) []string {
	query := map[string]bool{}
	for _, term := range queryTerms {
		query[term] = true
	}
	docFreq := map[string]int{}
	totalFreq := map[string]int{}
	feedback := min(len(results), prfFeedbackDocs)
	for _, result := range results[:feedback] {
		seen := map[string]bool{}
		for _, token := range strings.Fields(strings.ToLower(result.Title + " " + result.Snippet)) {
			if len(token) < prfMinTermLen || query[token] || pseudoRelevanceStopwords[token] {
				continue
			}
			totalFreq[token]++
			if !seen[token] {
				seen[token] = true
				docFreq[token]++
			}
		}
	}

	candidates := make([]string, 0, len(docFreq))
	for token, freq := range docFreq {
		if freq >= prfMinFeedbackDocFreq {
			candidates = append(candidates, token)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if docFreq[a] != docFreq[b] {
			return docFreq[a] > docFreq[b]
		}
		if totalFreq[a] != totalFreq[b] {
			return totalFreq[a] > totalFreq[b]
		}

		return a < b
	})
	if len(candidates) > prfExpansionTerms {
		candidates = candidates[:prfExpansionTerms]
	}

	return candidates
}

// pseudoRelevanceStopwords are high-frequency function words across the major
// languages the node serves; excluding them keeps expansion terms content-bearing
// even though the layer has no corpus IDF. It is intentionally small — content
// words dominate query-biased snippets, so a short list suffices.
var pseudoRelevanceStopwords = map[string]bool{
	// English
	"the": true, "and": true, "for": true, "with": true, "that": true,
	"this": true, "from": true, "have": true, "your": true, "you": true,
	"are": true, "was": true, "will": true, "not": true, "but": true,
	// German
	"und": true, "der": true, "die": true, "das": true, "den": true,
	"ein": true, "eine": true, "mit": true, "auf": true, "ist": true,
	// Spanish / French
	"que": true, "los": true, "las": true, "por": true, "para": true,
	"con": true, "una": true, "des": true, "les": true, "pour": true,
	"dans": true, "est": true, "sur": true,
	// Russian
	"это": true, "как": true, "что": true, "для": true, "или": true,
	"так": true, "все": true, "его": true, "она": true, "они": true,
}
