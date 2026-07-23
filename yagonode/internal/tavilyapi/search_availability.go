package tavilyapi

import (
	"context"
	"errors"
	"fmt"
)

var errSearchUnavailable = errors.New("search unavailable")

func searchAvailabilityError(resultCount, failureCount int, callerCause error) error {
	if resultCount != 0 || failureCount == 0 {
		return nil
	}
	if errors.Is(callerCause, context.Canceled) ||
		errors.Is(callerCause, context.DeadlineExceeded) {
		return nil
	}

	return fmt.Errorf("%w: one or more search sources did not complete", errSearchUnavailable)
}
