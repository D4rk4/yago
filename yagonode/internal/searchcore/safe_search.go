package searchcore

import (
	"context"
	"fmt"
)

type safeSearchSearcher struct {
	inner Searcher
}

func NewSafeSearchSearcher(inner Searcher) Searcher {
	return safeSearchSearcher{inner: inner}
}

func (s safeSearchSearcher) Search(ctx context.Context, req Request) (Response, error) {
	response, err := s.inner.Search(ctx, req)
	if err != nil {
		return Response{}, fmt.Errorf("safe search inner search: %w", err)
	}
	if !req.SafeSearch {
		return response, nil
	}
	filtered := make([]Result, 0, len(response.Results))
	for _, result := range response.Results {
		if allowsSafeResult(req, result) {
			filtered = append(filtered, result)
		}
	}
	if len(filtered) != len(response.Results) {
		response.TotalResults = len(filtered)
	}
	response.Results = filtered
	response.Request = req

	return response, nil
}

func allowsSafeResult(req Request, result Result) bool {
	if result.SafetyRating == SafetyExplicit {
		return false
	}
	unknown := result.SafetyRating != SafetyGeneral
	if unknown && (result.Source == SourceRemote || result.Source == SourceWeb) {
		return false
	}
	if unknown && (req.ContentDomain == ContentDomainImage ||
		result.ContentDomain == ContentDomainImage) {
		return false
	}

	return true
}
