package searchcore

import (
	"context"
	"errors"
)

type FailureStage uint8

const (
	FailureStageUnknown FailureStage = iota
	FailureStageLocalSearch
	FailureStageRemoteSearch
	FailureStageRemotePeer
	FailureStageRemoteStage
	FailureStagePeerReputation
	FailureStageExactSearch
	FailureStageLocalExactSearch
	FailureStageFuzzySearch
	FailureStageLocalEvidence
	FailureStageWebSearch
	FailureStageQueryShape
)

type FailureCause uint8

const (
	FailureCauseUnknown FailureCause = iota
	FailureCauseNoTarget
	FailureCauseDeadline
	FailureCauseCapacity
	FailureCauseBudget
	FailureCauseUnavailable
	FailureCauseBackend
	FailureCauseCanceled
	FailureCauseUnsupported
)

type PartialFailureDiagnostic struct {
	Stage FailureStage
	Cause FailureCause
}

func FailureCauseFor(err error) FailureCause {
	switch {
	case err == nil:
		return FailureCauseUnknown
	case errors.Is(err, context.DeadlineExceeded):
		return FailureCauseDeadline
	case errors.Is(err, context.Canceled):
		return FailureCauseCanceled
	default:
		return FailureCauseBackend
	}
}

func FailureStageForSource(source string) FailureStage {
	switch source {
	case PartialFailureSourceLocalSearch:
		return FailureStageLocalSearch
	case PartialFailureSourceRemoteYaCy:
		return FailureStageRemoteSearch
	case PartialFailureSourceRemoteStage:
		return FailureStageRemoteStage
	case PartialFailureSourcePeerReputation:
		return FailureStagePeerReputation
	case PartialFailureSourceExactStage:
		return FailureStageExactSearch
	case PartialFailureSourceLocalExactStage:
		return FailureStageLocalExactSearch
	case PartialFailureSourceFuzzyStage:
		return FailureStageFuzzySearch
	case PartialFailureSourceLocalEvidence:
		return FailureStageLocalEvidence
	case PartialFailureSourceWeb:
		return FailureStageWebSearch
	case PartialFailureSourceQueryShape:
		return FailureStageQueryShape
	default:
		return FailureStageUnknown
	}
}
