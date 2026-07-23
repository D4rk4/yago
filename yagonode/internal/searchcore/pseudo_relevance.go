package searchcore

import (
	"context"
	"fmt"
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
	prfActivateBelow         = 50
	prfMaximumDocumentTokens = 256
	prfMaximumQueryTokens    = 32
	prfMaximumTermRunes      = 48
	prfOriginalQueryWeight   = 0.5
)

// NewPseudoRelevanceSearcher expands a recall-poor query with terms mined from
// its own top results (pseudo-relevance feedback, RM3) and fuses the expanded
// pass with the original by reciprocal rank. The mined terms travel as
// Request.ExpansionTerms — optional scoring evidence, never required matches —
// so the second pass reorders documents that contain the actual query but can
// never surface one that only matches an expansion word (the classic RM3 drift
// failure). It wraps the local searcher: peers run their own retrieval, and a
// full first page skips expansion entirely.
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
	if len(first.Results) == 0 || len(first.Results) >= pseudoRelevanceActivationLimit(req.Limit) {
		return first, nil
	}
	expansion := minePseudoRelevanceTerms(
		first.Results,
		pseudoRelevanceQueryTerms(req),
		req.ExcludedTerms,
	)
	if len(expansion) == 0 {
		return first, nil
	}

	expanded := req
	expanded.ExpansionTerms = expansion
	second, err := s.inner.Search(ctx, expanded)
	if err != nil {
		// Expansion is best-effort: a failed second pass keeps the first result.
		return first, nil //nolint:nilerr // the original answer stands.
	}
	for index := range second.Results {
		second.Results[index].Evidence = second.Results[index].Evidence.With(
			SignalFeedbackRank,
			float64(index+1),
		).With(SignalFeedbackScore, second.Results[index].Score)
	}

	first.Results = FuseByReciprocalRank(first.Results, second.Results)

	return first, nil
}
