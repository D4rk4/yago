package yagonode

import (
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const localExactCapacityRecoveryBudget = 500 * time.Millisecond

func withLocalExactRecoveryBudgetForFailure(
	inner searchcore.Searcher,
	response searchcore.Response,
) searchcore.Searcher {
	budget := localExactRecoveryBudget
	wait := hasPartialFailure(
		response.PartialFailures,
		webFallbackExactStageFailureSource,
		webFallbackExactStageCapacityFailure,
	)
	if wait {
		budget = localExactCapacityRecoveryBudget
	}

	recovery := newLocalExactRecoveryBudget(inner, budget)
	recovery.waitForAdmission = wait

	return recovery
}

func hasPartialFailure(
	failures []searchcore.PartialFailure,
	source string,
	reason string,
) bool {
	for _, failure := range failures {
		if failure.Source == source && failure.Reason == reason {
			return true
		}
	}

	return false
}
