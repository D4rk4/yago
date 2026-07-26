package tavilyapi

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

const maximumConcurrentExtractURLResolutions = 4

const msgExtractURLResolutionPanicked = "extract URL resolution panicked"

var errExtractURLResolutionPanicked = errors.New("extract URL resolution failed")

type extractURLResolutionJob struct {
	position     int
	requestedURL string
}

type extractURLResolutionOutcome struct {
	position   int
	resolution extractURLResolution
	err        error
}

func (e extractEndpoint) resolveExtractURLs(
	ctx context.Context,
	req ExtractRequest,
	budget *rawContentBudget,
) ([]ExtractResult, []ExtractFailure, error) {
	distinct, sources := distinctExtractURLs(req.URLs)
	resolutionContext, cancel := context.WithCancel(ctx)
	halt := make(chan struct{})
	workerTotal := min(maximumConcurrentExtractURLResolutions, len(distinct))
	jobs := make(chan extractURLResolutionJob, workerTotal)
	outcomes := make(chan extractURLResolutionOutcome, workerTotal)
	var workers sync.WaitGroup
	workers.Add(workerTotal)
	for range workerTotal {
		go e.runExtractURLResolutionWorker(
			resolutionContext,
			halt,
			jobs,
			outcomes,
			&workers,
		)
	}
	defer func() {
		cancel()
		close(halt)
		workers.Wait()
	}()

	nextDispatch := 0
	for nextDispatch < workerTotal {
		jobs <- extractURLResolutionJob{
			position:     nextDispatch,
			requestedURL: distinct[nextDispatch],
		}
		nextDispatch++
	}
	results := make([]ExtractResult, 0, len(req.URLs))
	failures := make([]ExtractFailure, 0, len(req.URLs))
	resolved := make(map[int]extractURLResolution, len(distinct))
	for position := 0; position < len(sources); {
		resolution, ready := resolved[sources[position]]
		if !ready {
			outcome := <-outcomes
			if outcome.err != nil {
				return nil, nil, outcome.err
			}
			resolved[outcome.position] = outcome.resolution

			continue
		}
		// retain does not mutate the resolution, so a URL repeated in the
		// request yields its own row and consumes its own response budget while
		// the lookup or fetch behind it happened once.
		result, failure, err := resolution.retain(req, budget)
		if err != nil {
			return nil, nil, err
		}
		if failure != nil {
			failures = append(failures, *failure)
		} else {
			results = append(results, result)
		}
		position++
		if nextDispatch < len(distinct) {
			jobs <- extractURLResolutionJob{
				position:     nextDispatch,
				requestedURL: distinct[nextDispatch],
			}
			nextDispatch++
		}
	}

	return results, failures, nil
}

// distinctExtractURLs splits the requested URLs into the distinct set to resolve
// and, for each input position in order, the index of the distinct URL behind
// it. Duplicate inputs previously each paid for their own document lookup and
// possible outbound fetch; they now share one resolution while the response
// keeps every requested position, in input order.
func distinctExtractURLs(urls []string) (distinct []string, sources []int) {
	distinct = make([]string, 0, len(urls))
	sources = make([]int, 0, len(urls))
	seen := make(map[string]int, len(urls))
	for _, requestedURL := range urls {
		index, known := seen[requestedURL]
		if !known {
			index = len(distinct)
			seen[requestedURL] = index
			distinct = append(distinct, requestedURL)
		}
		sources = append(sources, index)
	}

	return distinct, sources
}

func (e extractEndpoint) runExtractURLResolutionWorker(
	ctx context.Context,
	halt <-chan struct{},
	jobs <-chan extractURLResolutionJob,
	outcomes chan<- extractURLResolutionOutcome,
	workers *sync.WaitGroup,
) {
	defer workers.Done()
	for {
		select {
		case <-halt:
			return
		case job := <-jobs:
			resolution, err := e.resolveExtractURLWithoutPanic(ctx, job.requestedURL)
			outcome := extractURLResolutionOutcome{
				position:   job.position,
				resolution: resolution,
				err:        err,
			}
			outcomes <- outcome
		}
	}
}

func (e extractEndpoint) resolveExtractURLWithoutPanic(
	ctx context.Context,
	requestedURL string,
) (resolution extractURLResolution, err error) {
	defer func() {
		if recover() != nil {
			slog.ErrorContext(ctx, msgExtractURLResolutionPanicked)
			resolution = extractURLResolution{}
			err = errExtractURLResolutionPanicked
		}
	}()

	return e.resolveExtractURL(ctx, requestedURL)
}
