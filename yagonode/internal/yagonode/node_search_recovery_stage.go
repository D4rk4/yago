package yagonode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

var (
	recoverySearchBudget           = 150 * time.Millisecond
	processRecoverySearchAdmission = newInteractiveSearchAdmission(
		interactiveSearchConcurrentWork,
	)
)

const (
	recoverySearchCancellationGrace = 25 * time.Millisecond
	recoverySearchFailureSource     = searchcore.PartialFailureSourceFuzzyStage
	recoverySearchTimeoutFailure    = "fuzzy search deadline exceeded"
	recoverySearchCapacityFailure   = "fuzzy search capacity exhausted"
	recoverySearchFailed            = "fuzzy search failed"
	recoverySearchPanicMessage      = "fuzzy search stage panicked"
	recoveryStageFailedLogMessage   = "bounded search recovery failed"
)

type recoveryBudgetSearcher struct {
	inner            searchcore.Searcher
	budget           time.Duration
	grace            time.Duration
	admission        *interactiveSearchAdmission
	waitForAdmission bool
	panicLog         func(context.Context, string, ...any)
	profile          recoveryStageProfile
}

type recoveryStageProfile struct {
	operation       string
	failureSource   string
	timeoutFailure  string
	capacityFailure string
	failedMessage   string
	panicMessage    string
}

type recoverySearchOutcome struct {
	response searchcore.Response
	err      error
	failure  any
}

func withRecoverySearchBudget(inner searchcore.Searcher) searchcore.Searcher {
	return recoveryBudgetSearcher{
		inner:     inner,
		budget:    recoverySearchBudget,
		grace:     recoverySearchCancellationGrace,
		admission: processRecoverySearchAdmission,
		panicLog:  slog.ErrorContext,
	}
}

func (s recoveryBudgetSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	profile := s.effectiveProfile()
	hardContext, hardCancel := context.WithTimeout(ctx, s.budget)
	defer hardCancel()
	stageBudget := s.budget - s.grace
	if stageBudget <= 0 {
		stageBudget = s.budget / 2
	}
	stageContext, stageCancel := context.WithTimeout(hardContext, stageBudget)
	defer stageCancel()

	acquire := s.admission.tryAcquire
	if s.waitForAdmission {
		acquire = s.admission.acquire
	}
	release, err := acquire(stageContext)
	if err != nil {
		if errors.Is(err, errInteractiveSearchCapacity) {
			return recoverySearchFailure(
				req, profile, profile.capacityFailure, searchcore.FailureCauseCapacity,
			), nil
		}
		if errors.Is(err, context.DeadlineExceeded) && context.Cause(ctx) == nil {
			return recoverySearchFailure(
				req, profile, profile.timeoutFailure, searchcore.FailureCauseDeadline,
			), nil
		}

		return searchcore.Response{}, fmt.Errorf("%s admission: %w", profile.operation, err)
	}
	outcomes := make(chan recoverySearchOutcome, 1)
	go s.run(stageContext, req, release, outcomes)

	select {
	case outcome := <-outcomes:
		return recoverySearchOutcomeResult(ctx, req, profile, outcome), nil
	case <-hardContext.Done():
		if ctx.Err() != nil {
			return searchcore.Response{}, fmt.Errorf("%s: %w", profile.operation, ctx.Err())
		}

		return recoverySearchFailure(
			req, profile, profile.timeoutFailure, searchcore.FailureCauseDeadline,
		), nil
	}
}

func recoverySearchOutcomeResult(
	ctx context.Context,
	req searchcore.Request,
	profile recoveryStageProfile,
	outcome recoverySearchOutcome,
) searchcore.Response {
	if outcome.failure != nil {
		panic(outcome.failure)
	}
	if outcome.err == nil {
		return outcome.response
	}
	outcome.response.Request = req
	if errors.Is(outcome.err, context.DeadlineExceeded) {
		outcome.response.PartialFailures = append(
			outcome.response.PartialFailures,
			searchcore.PartialFailure{
				Source: profile.failureSource,
				Reason: profile.timeoutFailure,
				Diagnostic: searchcore.PartialFailureDiagnostic{
					Stage: searchcore.FailureStageForSource(profile.failureSource),
					Cause: searchcore.FailureCauseDeadline,
				},
			},
		)

		return outcome.response
	}

	cause := searchcore.FailureCauseFor(outcome.err)
	slog.WarnContext(
		ctx,
		recoveryStageFailedLogMessage,
		slog.String("stage", profile.failureSource),
		slog.String("cause", searchcore.FailureCauseLabel(cause)),
		tracectx.ServerSpanAttribute(ctx),
	)
	outcome.response.PartialFailures = append(
		outcome.response.PartialFailures,
		searchcore.PartialFailure{
			Source: profile.failureSource,
			Reason: profile.failedMessage,
			Diagnostic: searchcore.PartialFailureDiagnostic{
				Stage: searchcore.FailureStageForSource(profile.failureSource),
				Cause: cause,
			},
		},
	)

	return outcome.response
}

func (s recoveryBudgetSearcher) run(
	ctx context.Context,
	req searchcore.Request,
	release func(),
	outcomes chan<- recoverySearchOutcome,
) {
	outcome := recoverySearchOutcome{}
	defer func() {
		outcome.failure = recover()
		if outcome.failure != nil {
			s.panicLog(ctx, s.effectiveProfile().panicMessage, tracectx.ServerSpanAttribute(ctx))
		}
		release()
		outcomes <- outcome
	}()
	outcome.response, outcome.err = s.inner.Search(ctx, req)
}

func recoverySearchFailure(
	req searchcore.Request,
	profile recoveryStageProfile,
	reason string,
	cause searchcore.FailureCause,
) searchcore.Response {
	return searchcore.Response{
		Request: req,
		PartialFailures: []searchcore.PartialFailure{{
			Source: profile.failureSource,
			Reason: reason,
			Diagnostic: searchcore.PartialFailureDiagnostic{
				Stage: searchcore.FailureStageForSource(profile.failureSource),
				Cause: cause,
			},
		}},
	}
}

func (s recoveryBudgetSearcher) effectiveProfile() recoveryStageProfile {
	if s.profile.failureSource != "" {
		return s.profile
	}

	return recoveryStageProfile{
		operation:       "fuzzy search",
		failureSource:   recoverySearchFailureSource,
		timeoutFailure:  recoverySearchTimeoutFailure,
		capacityFailure: recoverySearchCapacityFailure,
		failedMessage:   recoverySearchFailed,
		panicMessage:    recoverySearchPanicMessage,
	}
}
