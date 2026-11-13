package searchremote

import (
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

type peerSearchReply struct {
	response      yagoproto.SearchResponse
	countDeclared bool
}

type peerAnswerAvailability struct {
	answered             bool
	peerFailurePositions []int
}

func wholeQueryPeerResults(results []peerSearchResult) []peerSearchResult {
	for position := range results {
		results[position].wholeQuery = true
	}

	return results
}

func (availability *peerAnswerAvailability) appendFailure(
	failures []searchcore.PartialFailure,
	result peerSearchResult,
	err error,
) []searchcore.PartialFailure {
	failure := peerFailure(result.peer, err)
	if result.wholeQuery && failure.Diagnostic.Stage == searchcore.FailureStageRemotePeer &&
		searchcore.FailureStageForSource(failure.Source) == searchcore.FailureStageUnknown {
		availability.peerFailurePositions = append(
			availability.peerFailurePositions,
			len(failures),
		)
	}

	return append(failures, failure)
}

func (availability *peerAnswerAvailability) observeAnswer(
	result peerSearchResult,
	invalid bool,
) {
	if result.wholeQuery && result.countDeclared && !invalid && !result.resourcesTruncated {
		availability.answered = true
	}
}

func (availability peerAnswerAvailability) markAnsweredFailures(
	failures []searchcore.PartialFailure,
) {
	if !availability.answered {
		return
	}
	for _, position := range availability.peerFailurePositions {
		failures[position].Diagnostic.SourceAnswered = true
	}
}

func retractContestedPeerAnswers(failures []searchcore.PartialFailure, answered bool) {
	unanswered := make(map[string]struct{})
	for _, failure := range failures {
		if failure.Diagnostic.Stage == searchcore.FailureStageRemotePeer &&
			!failure.Diagnostic.SourceAnswered {
			unanswered[failure.Source] = struct{}{}
		}
	}
	for position := range failures {
		if _, found := unanswered[failures[position].Source]; !answered || found {
			failures[position].Diagnostic.SourceAnswered = false
		}
	}
}
