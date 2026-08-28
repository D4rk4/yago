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
	// The error path below reads response.PartialFailures on purpose. Response is
	// a struct value, not a pointer, so on that path it is the zero value and the
	// field reads as a nil slice: there is nothing to dereference and the panic
	// the rule warns about cannot occur.
	// nosemgrep: trailofbits.go.invalid-usage-of-modified-variable.invalid-usage-of-modified-variable
	response, err := s.inner.Search(ctx, req)
	if err != nil {
		// Keep the partial failures the federation deliberately returned
		// alongside its error. Dropping them replaced four named peer failures
		// with one opaque stage timeout, losing the diagnosis exactly when the
		// answer is empty and the operator most needs to know who did not
		// answer. Results are still discarded: they were never filtered.
		return Response{Request: req, PartialFailures: response.PartialFailures},
			fmt.Errorf("safe search inner search: %w", err)
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
	if result.SafetyRating == SafetyProviderFiltered {
		return result.Source == SourceWeb
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
