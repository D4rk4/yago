package searchlocal

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

const (
	candidateFusionK                = 60
	maximumProximityCandidateWindow = 200
	proximityCandidateMultiplier    = 2
)

func (s localSearcher) searchCandidates(
	ctx context.Context,
	req searchindex.SearchRequest,
) (searchindex.SearchResultSet, error) {
	originalLimit := req.MaxResults
	req.MaxResults = proximityCandidateLimit(req)
	minimum := relaxedMinimumTermMatches(req)
	strictRequest := req
	if minimum > 0 && req.WithFacets {
		strictRequest.WithFacets = false
	}
	strict, err := s.index.Search(ctx, strictRequest)
	if err != nil {
		return searchindex.SearchResultSet{}, fmt.Errorf("strict candidates: %w", err)
	}
	if minimum == 0 {
		return strict, nil
	}

	relaxedRequest := req
	relaxedRequest.MinimumTermMatches = minimum
	relaxed, err := s.index.Search(ctx, relaxedRequest)
	if err != nil {
		return searchindex.SearchResultSet{}, fmt.Errorf("relaxed candidates: %w", err)
	}

	return fuseCandidateSets(strict, relaxed, max(originalLimit, req.MaxResults)), nil
}

func proximityCandidateLimit(req searchindex.SearchRequest) int {
	if req.Fuzzy || req.MaxResults <= 0 {
		return req.MaxResults
	}
	terms := distinctCandidateTerms(req)
	if len(terms) < 2 {
		return req.MaxResults
	}
	if req.MaxResults >= maximumProximityCandidateWindow {
		return req.MaxResults
	}

	return min(maximumProximityCandidateWindow, req.MaxResults*proximityCandidateMultiplier)
}

func distinctCandidateTerms(req searchindex.SearchRequest) map[string]struct{} {
	terms := req.Terms
	if len(terms) == 0 {
		terms = strings.Fields(req.Query)
	}
	distinct := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(strings.ToLower(term))
		if term != "" {
			distinct[term] = struct{}{}
		}
	}

	return distinct
}

func relaxedMinimumTermMatches(req searchindex.SearchRequest) int {
	if req.Fuzzy || req.Near || req.MinimumTermMatches > 0 {
		return 0
	}
	count := len(distinctCandidateTerms(req))
	if count < 2 {
		return 0
	}
	minimum := int(math.Ceil(float64(count) * 0.6))

	return max(1, min(count-1, minimum))
}

func fuseCandidateSets(
	strict searchindex.SearchResultSet,
	relaxed searchindex.SearchResultSet,
	limit int,
) searchindex.SearchResultSet {
	results := make([]searchindex.SearchResult, 0, len(strict.Results)+len(relaxed.Results))
	positions := make(map[string]int, cap(results))
	weights := make(map[string]float64, cap(results))
	addCandidateBranch(&results, positions, weights, strict.Results, true)
	addCandidateBranch(&results, positions, weights, relaxed.Results, false)
	for identity, position := range positions {
		results[position].Score = weights[identity]
	}
	slices.SortStableFunc(results, func(left, right searchindex.SearchResult) int {
		return cmp.Or(
			cmp.Compare(right.Score, left.Score),
			strings.Compare(candidateIdentity(left), candidateIdentity(right)),
		)
	})
	if limit >= 0 && len(results) > limit {
		results = results[:limit]
	}

	return searchindex.SearchResultSet{
		Results: results,
		Total:   max(relaxed.Total, len(results)),
		Facets:  relaxed.Facets,
	}
}

func addCandidateBranch(
	results *[]searchindex.SearchResult,
	positions map[string]int,
	weights map[string]float64,
	branch []searchindex.SearchResult,
	strict bool,
) {
	seen := make(map[string]struct{}, len(branch))
	for position, result := range branch {
		identity := candidateIdentity(result)
		if _, found := seen[identity]; found {
			continue
		}
		seen[identity] = struct{}{}
		resultPosition, found := positions[identity]
		if !found {
			resultPosition = len(*results)
			positions[identity] = resultPosition
			*results = append(*results, result)
		}
		stored := &(*results)[resultPosition]
		if strict {
			stored.StrictScore = result.Score
			stored.StrictRank = position + 1
		} else {
			stored.RelaxedScore = result.Score
			stored.RelaxedRank = position + 1
		}
		weights[identity] += 1 / float64(candidateFusionK+position+1)
	}
}

func candidateIdentity(result searchindex.SearchResult) string {
	if result.DocumentID != "" {
		return "document:" + result.DocumentID
	}

	return "url:" + result.URL
}
