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
	resolutionContext, cancel := context.WithCancel(ctx)
	halt := make(chan struct{})
	workerTotal := min(maximumConcurrentExtractURLResolutions, len(req.URLs))
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
		close(jobs)
		workers.Wait()
	}()

	nextDispatch := 0
	for nextDispatch < workerTotal {
		jobs <- extractURLResolutionJob{
			position:     nextDispatch,
			requestedURL: req.URLs[nextDispatch],
		}
		nextDispatch++
	}
	results := make([]ExtractResult, 0, len(req.URLs))
	failures := make([]ExtractFailure, 0, len(req.URLs))
	pending := make(map[int]extractURLResolution, workerTotal)
	for nextPosition := 0; nextPosition < len(req.URLs); {
		resolution, ready := pending[nextPosition]
		if !ready {
			outcome := <-outcomes
			if outcome.err != nil {
				return nil, nil, outcome.err
			}
			pending[outcome.position] = outcome.resolution

			continue
		}
		result, failure, err := resolution.retain(req, budget)
		if err != nil {
			return nil, nil, err
		}
		if failure != nil {
			failures = append(failures, *failure)
		} else {
			results = append(results, result)
		}
		delete(pending, nextPosition)
		nextPosition++
		if nextDispatch < len(req.URLs) {
			jobs <- extractURLResolutionJob{
				position:     nextDispatch,
				requestedURL: req.URLs[nextDispatch],
			}
			nextDispatch++
		}
	}

	return results, failures, nil
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
		case job, open := <-jobs:
			if !open {
				return
			}
			resolution, err := e.resolveExtractURLWithoutPanic(ctx, job.requestedURL)
			outcome := extractURLResolutionOutcome{
				position:   job.position,
				resolution: resolution,
				err:        err,
			}
			select {
			case outcomes <- outcome:
			case <-halt:
				return
			}
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
