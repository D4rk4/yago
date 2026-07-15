package searchcore

import "time"

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
