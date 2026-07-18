package searchvisible

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

const maximumVisibleEvidenceCandidates = searchcore.MaximumPublicResultHorizon

type visibleEvidenceSearcher struct {
	inner searchcore.Searcher
}

func NewVisibleEvidenceSearcher(inner searchcore.Searcher) searchcore.Searcher {
	return visibleEvidenceSearcher{inner: inner}
}

func (s visibleEvidenceSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	response, err := s.inner.Search(ctx, req)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("visible evidence inner search: %w", err)
	}
	terms := req.Terms
	if len(terms) == 0 {
		terms = strings.Fields(req.Query)
	}
	query, available := searchindex.NewVisibleTextQuery(terms)
	if !available {
		return response, nil
	}
	limit := min(len(response.Results), maximumVisibleEvidenceCandidates)
	for index := range limit {
		if ctx.Err() != nil {
			return completedVisibleEvidence(response)
		}
		result := response.Results[index]
		if result.EvidenceReady {
			continue
		}
		evidence, available, err := query.Analyze(
			ctx,
			result.Language,
			searchindex.VisibleText{
				Title:   result.Title,
				Snippet: result.Snippet,
				URL:     decodedVisibleURL(result.URL),
			},
		)
		if err != nil {
			return completedVisibleEvidence(response)
		}
		if !available {
			continue
		}
		result.Analyzer = evidence.Analyzer
		result.EvidenceReady = true
		result.EvidenceRequirementOrdinals = append(
			[]int{},
			evidence.EvidenceRequirementOrdinals...,
		)
		result.FieldTermPositions = evidence.FieldTermPositions
		result.QueryMatches = coreQueryMatches(evidence.QueryMatches)
		response.Results[index] = result
	}

	return response, nil
}

func completedVisibleEvidence(response searchcore.Response) (searchcore.Response, error) {
	return response, nil
}

func decodedVisibleURL(rawURL string) string {
	decoded, err := url.PathUnescape(rawURL)
	if err != nil {
		return rawURL
	}

	return decoded
}

func coreQueryMatches(matches []searchindex.TextQueryMatch) []searchcore.QueryMatch {
	mapped := make([]searchcore.QueryMatch, len(matches))
	for index, match := range matches {
		mapped[index] = searchcore.QueryMatch{Start: match.Start, End: match.End}
	}

	return mapped
}
