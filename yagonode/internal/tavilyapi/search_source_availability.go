package tavilyapi

import "github.com/D4rk4/yago/yagonode/internal/searchcore"

func searchSourceLosses(response searchcore.Response) int {
	lost := 0
	for _, failure := range response.PartialFailures {
		if optionalSearchFailure(failure) {
			continue
		}
		lost++
	}

	return lost
}

func optionalSearchFailure(failure searchcore.PartialFailure) bool {
	switch failure.Source {
	case searchcore.PartialFailureSourceQueryShape:
		return true
	case searchcore.PartialFailureSourceFuzzyStage:
		return failure.Diagnostic.Cause == searchcore.FailureCauseDeadline
	default:
		return searchcore.FailureStageForSource(failure.Source) == searchcore.FailureStageUnknown &&
			failure.Diagnostic.Stage == searchcore.FailureStageRemotePeer &&
			failure.Diagnostic.SourceAnswered
	}
}
