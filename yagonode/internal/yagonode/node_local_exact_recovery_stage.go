package yagonode

import (
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

var (
	localExactRecoveryBudget           = 150 * time.Millisecond
	processLocalExactRecoveryAdmission = newInteractiveSearchAdmission(
		interactiveSearchConcurrentWork,
	)
)

const (
	localExactRecoveryCancellationGrace = 25 * time.Millisecond
	localExactRecoveryFailureSource     = searchcore.PartialFailureSourceLocalExactStage
	localExactRecoveryTimeoutFailure    = "local exact search deadline exceeded"
	localExactRecoveryCapacityFailure   = "local exact search capacity exhausted"
	localExactRecoveryFailed            = "local exact search failed"
	localExactRecoveryPanicMessage      = "local exact search stage panicked"
)

var localExactRecoveryProfile = recoveryStageProfile{
	operation:       "local exact search",
	failureSource:   localExactRecoveryFailureSource,
	timeoutFailure:  localExactRecoveryTimeoutFailure,
	capacityFailure: localExactRecoveryCapacityFailure,
	failedMessage:   localExactRecoveryFailed,
	panicMessage:    localExactRecoveryPanicMessage,
}

func withLocalExactRecoveryBudget(inner searchcore.Searcher) searchcore.Searcher {
	return recoveryBudgetSearcher{
		inner:     inner,
		budget:    localExactRecoveryBudget,
		grace:     localExactRecoveryCancellationGrace,
		admission: processLocalExactRecoveryAdmission,
		panicLog:  slog.ErrorContext,
		profile:   localExactRecoveryProfile,
	}
}
