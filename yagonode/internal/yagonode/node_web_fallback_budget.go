package yagonode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

var (
	webFallbackExactStageBudget           = 600 * time.Millisecond
	webFallbackProviderBudget             = 900 * time.Millisecond
	webFallbackParallelProviderBudget     = 1500 * time.Millisecond
	processWebFallbackExactStageAdmission = newInteractiveSearchAdmission(
		interactiveSearchConcurrentWork,
	)
)

const (
	webFallbackExactStageCancellationGrace = 50 * time.Millisecond
	webFallbackExactStageFailureSource     = searchcore.PartialFailureSourceExactStage
	webFallbackExactStageTimeoutFailure    = "exact search deadline exceeded"
	webFallbackExactStageCapacityFailure   = "exact search capacity exhausted"
	webFallbackExactStageFailed            = "exact search failed"
	webFallbackExactStagePanicMessage      = "exact search stage panicked"
	webFallbackExactStageFailedLogMessage  = "bounded exact search failed"
)

type webFallbackExactStageBudgetSearcher struct {
	inner     searchcore.Searcher
	permit    func(searchcore.Request) bool
	budget    time.Duration
	grace     time.Duration
	admission *interactiveSearchAdmission
	panicLog  func(context.Context, string, ...any)
}

type webFallbackExactStageOutcome struct {
	response searchcore.Response
	err      error
	failure  any
}

func withWebFallbackExactStageBudget(
	inner searchcore.Searcher,
	config webFallbackConfig,
) searchcore.Searcher {
	if inner == nil || config.Provider != webFallbackProviderDDGS ||
		config.Privacy == webFallbackPrivacyDisabled {
		return inner
	}

	return webFallbackExactStageBudgetSearcher{
		inner:     inner,
		permit:    webFallbackPermit(config.Privacy),
		budget:    webFallbackExactStageBudget,
		grace:     webFallbackExactStageCancellationGrace,
		admission: processWebFallbackExactStageAdmission,
		panicLog:  slog.ErrorContext,
	}
}

func (s webFallbackExactStageBudgetSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	budgeted := s.permit(req) &&
		(req.Source != searchcore.SourceLocal || req.AllowWebFallback) &&
		(req.ContentDomain == "" || req.ContentDomain == searchcore.ContentDomainText) &&
		strings.TrimSpace(req.SubmittedText()) != ""
	if !budgeted {
		response, err := s.inner.Search(ctx, req)
		if err != nil {
			return searchcore.Response{}, fmt.Errorf("search exact stage: %w", err)
		}

		return response, nil
	}

	hardContext, hardCancel := context.WithTimeout(ctx, s.budget)
	defer hardCancel()
	stageBudget := s.budget - s.grace
	if stageBudget <= 0 {
		stageBudget = s.budget / 2
	}
	stageContext, stageCancel := context.WithTimeout(hardContext, stageBudget)
	defer stageCancel()

	release, err := s.admission.tryAcquire(stageContext)
	if err != nil {
		if errors.Is(err, errInteractiveSearchCapacity) {
			return webFallbackExactStageFailure(req, webFallbackExactStageCapacityFailure), nil
		}

		return webFallbackExactStageResult(ctx, req, searchcore.Response{}, err)
	}
	outcomes := make(chan webFallbackExactStageOutcome, 1)
	go s.run(stageContext, req, release, outcomes)

	select {
	case outcome := <-outcomes:
		if outcome.failure != nil {
			panic(outcome.failure)
		}
		if outcome.err == nil {
			return outcome.response, nil
		}

		return webFallbackExactStageResult(ctx, req, outcome.response, outcome.err)
	case <-hardContext.Done():
		return webFallbackExactStageResult(
			ctx,
			req,
			searchcore.Response{},
			context.Cause(hardContext),
		)
	}
}

func webFallbackExactStageResult(
	ctx context.Context,
	req searchcore.Request,
	response searchcore.Response,
	err error,
) (searchcore.Response, error) {
	if cause := context.Cause(ctx); cause != nil {
		return response, fmt.Errorf("search exact stage: %w", cause)
	}
	reason := webFallbackExactStageFailed
	if errors.Is(err, context.DeadlineExceeded) {
		reason = webFallbackExactStageTimeoutFailure
	} else {
		slog.WarnContext(ctx, webFallbackExactStageFailedLogMessage,
			slog.Any("error", err),
		)
	}
	response.Request = req
	response.PartialFailures = append(
		response.PartialFailures,
		searchcore.PartialFailure{
			Source: webFallbackExactStageFailureSource,
			Reason: reason,
		},
	)

	return response, nil
}

func (s webFallbackExactStageBudgetSearcher) run(
	ctx context.Context,
	req searchcore.Request,
	release func(),
	outcomes chan<- webFallbackExactStageOutcome,
) {
	outcome := webFallbackExactStageOutcome{}
	defer func() {
		outcome.failure = recover()
		if outcome.failure != nil {
			s.panicLog(ctx, webFallbackExactStagePanicMessage, slog.Any("panic", outcome.failure))
		}
		release()
		outcomes <- outcome
	}()
	outcome.response, outcome.err = s.inner.Search(ctx, req)
}

func webFallbackExactStageFailure(
	req searchcore.Request,
	reason string,
) searchcore.Response {
	return searchcore.Response{
		Request: req,
		PartialFailures: []searchcore.PartialFailure{{
			Source: webFallbackExactStageFailureSource,
			Reason: reason,
		}},
	}
}
