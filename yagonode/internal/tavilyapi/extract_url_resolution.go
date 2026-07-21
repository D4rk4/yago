package tavilyapi

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	extractInvalidURLFailureMessage = "url must be an absolute http or https URL"
	extractMissingURLFailureMessage = "url is not in the index and fetch-on-extract is disabled"
	extractFetchFailureMessage      = "fetch-on-extract failed"
)

type extractURLResolution struct {
	requestedURL string
	document     *documentstore.Document
	fetched      *FetchedContent
	failure      string
}

func (e extractEndpoint) resolveExtractURL(
	ctx context.Context,
	requestedURL string,
) (extractURLResolution, error) {
	normalizedURL, valid := normalizeExtractURL(requestedURL)
	if !valid {
		return failedExtractURLResolution(requestedURL, extractInvalidURLFailureMessage), nil
	}
	document, found, err := e.lookupWithinExtractBudget(ctx, normalizedURL)
	if err != nil {
		return e.resolveExtractLookupError(ctx, requestedURL, normalizedURL, err)
	}
	if found {
		return extractURLResolution{requestedURL: requestedURL, document: &document}, nil
	}
	if e.fetcher == nil {
		return failedExtractURLResolution(requestedURL, extractMissingURLFailureMessage), nil
	}
	fetched, err := e.fetcher.Fetch(ctx, normalizedURL)
	if err != nil {
		return failedExtractURLResolutionResult(requestedURL, extractFetchFailureMessage)
	}

	return extractURLResolution{requestedURL: requestedURL, fetched: &fetched}, nil
}

func (e extractEndpoint) resolveExtractLookupError(
	ctx context.Context,
	requestedURL string,
	normalizedURL string,
	err error,
) (extractURLResolution, error) {
	if !extractDocumentLookupTimedOut(err) {
		return extractURLResolution{}, err
	}
	if ctx.Err() != nil || e.fetcher == nil {
		return failedExtractURLResolutionResult(requestedURL, extractTimeoutFailureMessage)
	}
	fetched, fetchErr := e.fetcher.Fetch(ctx, normalizedURL)
	if fetchErr != nil {
		return failedExtractURLResolutionResult(requestedURL, extractFetchFailureMessage)
	}

	return extractURLResolution{requestedURL: requestedURL, fetched: &fetched}, nil
}

func failedExtractURLResolutionResult(
	requestedURL string,
	message string,
) (extractURLResolution, error) {
	return failedExtractURLResolution(requestedURL, message), nil
}

func failedExtractURLResolution(requestedURL, message string) extractURLResolution {
	return extractURLResolution{requestedURL: requestedURL, failure: message}
}

func (r extractURLResolution) retain(
	req ExtractRequest,
	budget *rawContentBudget,
) (ExtractResult, *ExtractFailure, error) {
	if r.failure != "" {
		failure, err := retainExtractFailure(budget, r.requestedURL, r.failure)

		return ExtractResult{}, failure, err
	}
	if r.document != nil {
		return retainDocumentExtractResult(req, r.requestedURL, *r.document, budget)
	}

	return retainFetchedExtractResult(req, r.requestedURL, *r.fetched, budget)
}
