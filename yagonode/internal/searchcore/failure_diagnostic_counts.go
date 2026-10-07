package searchcore

import (
	"slices"
	"strings"
)

type FailureDiagnosticCount struct {
	Stage string `json:"stage"`
	Cause string `json:"cause"`
	Count int    `json:"count"`
}

func FailureDiagnosticCounts(failures []PartialFailure) []FailureDiagnosticCount {
	type key struct {
		stage string
		cause string
	}
	counts := make(map[key]int)
	for _, failure := range failures {
		if failure.Source == PartialFailureSourceQueryShape {
			continue
		}
		pair := key{
			stage: failureStageLabel(failure.Diagnostic.Stage),
			cause: FailureCauseLabel(failure.Diagnostic.Cause),
		}
		counts[pair]++
	}
	result := make([]FailureDiagnosticCount, 0, len(counts))
	for pair, count := range counts {
		result = append(result, FailureDiagnosticCount{
			Stage: pair.stage,
			Cause: pair.cause,
			Count: count,
		})
	}
	slices.SortFunc(result, func(left, right FailureDiagnosticCount) int {
		if left.Stage != right.Stage {
			return strings.Compare(left.Stage, right.Stage)
		}
		return strings.Compare(left.Cause, right.Cause)
	})

	return result
}

func failureStageLabel(stage FailureStage) string {
	switch stage {
	case FailureStageLocalSearch:
		return PartialFailureSourceLocalSearch
	case FailureStageRemoteSearch:
		return PartialFailureSourceRemoteYaCy
	case FailureStageRemotePeer:
		return "remote-peer"
	case FailureStageRemoteStage:
		return PartialFailureSourceRemoteStage
	case FailureStagePeerReputation:
		return PartialFailureSourcePeerReputation
	case FailureStageExactSearch:
		return PartialFailureSourceExactStage
	case FailureStageLocalExactSearch:
		return PartialFailureSourceLocalExactStage
	case FailureStageFuzzySearch:
		return PartialFailureSourceFuzzyStage
	case FailureStageLocalEvidence:
		return PartialFailureSourceLocalEvidence
	case FailureStageWebSearch:
		return (PartialFailure{Source: PartialFailureSourceWeb}).SourceLabel()
	default:
		return "other"
	}
}

func FailureCauseLabel(cause FailureCause) string {
	switch cause {
	case FailureCauseNoTarget:
		return "no-target"
	case FailureCauseDeadline:
		return "deadline"
	case FailureCauseCapacity:
		return "capacity"
	case FailureCauseBudget:
		return "budget"
	case FailureCauseUnavailable:
		return "unavailable"
	case FailureCauseBackend:
		return "backend"
	case FailureCauseCanceled:
		return "canceled"
	case FailureCauseUnsupported:
		return "unsupported"
	default:
		return "other"
	}
}
