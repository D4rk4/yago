package tavilyapi

import (
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
	if completion.err == nil {
		return nil
	}
	if availabilityErr := searchAvailabilityError(
		len(completion.response.Results),
		len(completion.response.PartialFailures),
		callerCause,
	); availabilityErr != nil {
		return availabilityErr
	}

	return fmt.Errorf("search failed: %w", completion.err)
}
