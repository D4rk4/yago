package searchcore

import (
	"context"
	"time"
)

const federatedCancellationGrace = 25 * time.Millisecond

func federatedBranchFailure(
	response Response,
	source string,
	reason string,
) Response {
	response.PartialFailures = append(response.PartialFailures, PartialFailure{
		Source: source,
		Reason: reason,
	})

	return response
}

func federatedBranchTotal(response Response, branchError error) int {
	if branchError != nil && len(response.Results) == 0 {
		return 0
	}

	return max(response.TotalResults, len(response.Results))
}

func awaitRemoteOutcome(
	ctx context.Context,
	outcomes <-chan searchOutcome,
	local Response,
) (searchOutcome, bool) {
	select {
	case outcome := <-outcomes:
		return outcome, true
	case <-ctx.Done():
		return remoteOutcomeAfterCancellation(outcomes, local)
	}
}

func remoteOutcomeAfterCancellation(
	outcomes <-chan searchOutcome,
	local Response,
) (searchOutcome, bool) {
	if outcome, ready := availableRemoteOutcome(outcomes); ready {
		return outcome, true
	}
	if len(local.Results) > 0 {
		return searchOutcome{}, false
	}

	return drainRemoteOutcome(outcomes)
}

func availableRemoteOutcome(outcomes <-chan searchOutcome) (searchOutcome, bool) {
	select {
	case outcome := <-outcomes:
		return outcome, true
	default:
		return searchOutcome{}, false
	}
}

func drainRemoteOutcome(outcomes <-chan searchOutcome) (searchOutcome, bool) {
	timer := time.NewTimer(federatedCancellationGrace)
	defer timer.Stop()
	select {
	case outcome := <-outcomes:
		return outcome, true
	case <-timer.C:
		return searchOutcome{}, false
	}
}
