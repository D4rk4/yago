package tavilyapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

var errSearchUnavailable = errors.New("search unavailable")

func searchAvailabilityFailureCount(response searchcore.Response) int {
	failures := response.LostSourceFailures()
	if response.Coverage != nil && !response.Coverage.Complete {
		failures++
	}

	return failures
}

func searchAvailabilityFailureCountForFirstSeen(response searchcore.Response) int {
	failures := 0
	for _, failure := range response.PartialFailures {
		switch failure.Source {
		case searchcore.PartialFailureSourceRemoteYaCy,
			searchcore.PartialFailureSourceRemoteStage,
			searchcore.PartialFailureSourcePeerReputation,
			searchcore.PartialFailureSourceQueryShape:
			continue
		default:
			failures++
		}
	}
	if response.Coverage != nil && !response.Coverage.Complete {
		failures++
	}

	return failures
}

// searchAvailabilityError reports that the node cannot vouch for an empty
// answer. retrievedCount must be the number of results the search sources
// returned, never the number left after the caller's own filters: a request
// whose date, domain, or exact-match constraints matched nothing is a complete
// answer with an empty result set, and answering it 503 tells the caller to
// retry a query whose outcome is deterministic.
func searchAvailabilityError(retrievedCount, failureCount int, callerCause error) error {
	if retrievedCount != 0 || failureCount == 0 {
		return nil
	}
	if errors.Is(callerCause, context.Canceled) ||
		errors.Is(callerCause, context.DeadlineExceeded) {
		return nil
	}

	return fmt.Errorf("%w: one or more search sources did not complete", errSearchUnavailable)
}
