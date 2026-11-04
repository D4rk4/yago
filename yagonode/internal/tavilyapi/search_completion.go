package tavilyapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type searchCompletion struct {
	response searchcore.Response
	err      error
}

func searchCompletionFrom(
	response searchcore.Response,
	err error,
) searchCompletion {
	return searchCompletion{response: response, err: err}
}

func (completion searchCompletion) errorForCaller(callerCause error) error {
	if callerCause != nil && !searchResponseComplete(completion.response) {
		if len(completion.response.Results) > 0 {
			return nil
		}
		if errors.Is(callerCause, context.DeadlineExceeded) {
			return searchAvailabilityError(0, 1, nil)
		}
		return fmt.Errorf("search failed: %w", callerCause)
	}
	if completion.err == nil {
		return nil
	}
	if availabilityErr := searchAvailabilityError(
		len(completion.response.Results),
		searchAvailabilityFailureCount(completion.response),
		callerCause,
	); availabilityErr != nil {
		return availabilityErr
	}
	if callerCause == nil && len(completion.response.Results) > 0 {
		return nil
	}

	return fmt.Errorf("search failed: %w", completion.err)
}

func searchResponseComplete(response searchcore.Response) bool {
	return len(response.PartialFailures) == 0 &&
		(response.Coverage == nil || response.Coverage.Complete)
}
