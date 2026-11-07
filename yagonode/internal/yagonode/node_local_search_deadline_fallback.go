package yagonode

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	localSearchFallbackReason = "randomized progressive shard retrieval returned incomplete coverage"
)

type localSearchDeadlineFallback struct {
	primary  searchcore.Searcher
	fallback searchcore.Searcher
}

func withLocalSearchDeadlineFallback(
	primary searchcore.Searcher,
	fallback searchcore.Searcher,
) searchcore.Searcher {
	return localSearchDeadlineFallback{primary: primary, fallback: fallback}
}

func (s localSearchDeadlineFallback) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if searchcore.FullShardSearchRequired(ctx) {
		return s.searchPrimary(ctx, req)
	}
	if s.fallback == nil || !progressiveFallbackEligible(req) {
		return s.searchPrimary(ctx, req)
	}
	if searchcore.SearchProgressObserverFrom(ctx) != nil {
		return s.runFallback(ctx, req, searchcore.Response{}), nil
	}
	if ctx.Err() != nil {
		return searchcore.Response{}, fmt.Errorf(
			"local search request: %w",
			context.Cause(ctx),
		)
	}
	response, err := s.searchPrimary(ctx, req)
	if err == nil && !localSearchNeedsRandomFallback(response) {
		return response, nil
	}
	_, callerHasDeadline := ctx.Deadline()
	if err == nil && ctx.Err() == nil ||
		errors.Is(err, context.DeadlineExceeded) && callerHasDeadline && ctx.Err() == nil {
		return s.runFallback(ctx, req, response), nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return response, fmt.Errorf("local search request: %w", cause)
	}
	if len(response.Results) > 0 {
		response.PartialFailures = append(
			response.PartialFailures,
			localSearchDeadlinePartialFailure(),
		)

		return response, nil
	}
	return response, err
}

func localSearchNeedsRandomFallback(response searchcore.Response) bool {
	if response.Coverage != nil && !response.Coverage.Complete {
		return true
	}
	for _, failure := range response.PartialFailures {
		if failure.Source == searchcore.PartialFailureSourceLocalSearch {
			return true
		}
	}

	return false
}

func (s localSearchDeadlineFallback) searchPrimary(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	response, err := s.primary.Search(ctx, req)
	if err != nil {
		return response, fmt.Errorf("full-text local search: %w", err)
	}

	return response, nil
}

func (s localSearchDeadlineFallback) runFallback(
	ctx context.Context,
	req searchcore.Request,
	primaryResponse searchcore.Response,
) searchcore.Response {
	fallbackResponse, err := s.fallback.Search(ctx, req) // nosemgrep
	if err != nil {
		if len(fallbackResponse.Results) == 0 && fallbackResponse.Coverage == nil {
			return incompleteAfterFallbackFailure(req, primaryResponse, err)
		}

		return incompleteAfterFallbackFailure(req, fallbackResponse, err)
	}
	fallbackResponse.Request = req

	return fallbackResponse
}

func incompleteAfterFallbackFailure(
	req searchcore.Request,
	response searchcore.Response,
	searchErr error,
) searchcore.Response {
	response.Request = req
	if errors.Is(searchErr, context.DeadlineExceeded) &&
		response.Coverage != nil && response.Coverage.SampledShards > 0 &&
		len(response.Results) > 0 {
		return response
	}
	response.PartialFailures = append(
		response.PartialFailures,
		localSearchDeadlinePartialFailure(),
	)

	return response
}

func localSearchDeadlinePartialFailure() searchcore.PartialFailure {
	return searchcore.PartialFailure{
		Source: searchcore.PartialFailureSourceLocalSearch,
		Reason: localSearchFallbackReason,
		Diagnostic: searchcore.PartialFailureDiagnostic{
			Stage: searchcore.FailureStageLocalSearch,
			Cause: searchcore.FailureCauseDeadline,
		},
	}
}

func progressiveFallbackEligible(req searchcore.Request) bool {
	if len(req.Terms) == 0 || len(req.Terms) > 4 || req.Offset != 0 ||
		!plainFallbackQuery(req.Query, len(req.Terms)) ||
		(req.SubmittedQuery != "" && !plainFallbackQuery(req.SubmittedQuery, len(req.Terms))) ||
		len(req.ExcludedTerms) != 0 || len(req.Phrases) != 0 ||
		req.Fuzzy || req.WithFacets || req.Near || req.SortByDate ||
		req.Explain || req.MinDate != (time.Time{}) ||
		req.MaxDate != (time.Time{}) || req.MinFirstSeen != (time.Time{}) ||
		req.MaxFirstSeen != (time.Time{}) || len(req.IncludeDomains) != 0 ||
		len(req.ExcludeDomains) != 0 || req.SiteHost != "" || req.Language != "" ||
		req.InURL != "" || req.TLD != "" || req.FileType != "" || req.Author != "" ||
		req.URLMaskFilter != "" || req.PreferMaskFilter != "" ||
		len(req.ExpansionTerms) != 0 || req.Navigation != "" ||
		req.Verify != "" && req.Verify != searchcore.VerifyIfExist {
		return false
	}
	switch req.ContentDomain {
	case "", searchcore.ContentDomainAll, searchcore.ContentDomainText:
		return true
	default:
		return false
	}
}

func plainFallbackQuery(query string, expectedTerms int) bool {
	words := strings.Fields(query)
	if expectedTerms <= 0 || len(words) != expectedTerms {
		return false
	}
	for _, word := range words {
		for _, character := range word {
			if !unicode.IsLetter(character) && !unicode.IsNumber(character) {
				return false
			}
		}
	}

	return true
}
