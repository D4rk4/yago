package clickcapture

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const (
	retainedImpressionPreparations          = 4
	retainedImpressionPersistenceFailedText = "retained impression persistence failed"
)

var (
	impressionPreparationBudget     = 50 * time.Millisecond
	errImpressionPreparationBusy    = errors.New("impression preparation is busy")
	errImpressionPreparationStopped = errors.New("impression preparation is stopped")
)

type impressionPreparation struct {
	prepared PreparedImpression
	persist  func(context.Context) error
	expires  time.Time
}

type impressionPreparationOutcome struct {
	prepared PreparedImpression
	err      error
}

type impressionPreparationTask struct {
	responseContext    context.Context
	persistenceContext context.Context
	prepare            func() (impressionPreparation, error)
	planned            chan<- impressionPreparation
	completed          chan<- impressionPreparationOutcome
	abandoned          <-chan struct{}
}

type impressionPreparationLifecycle struct {
	state              sync.Mutex
	admission          chan struct{}
	persistences       map[impressionIdentity]*impressionPersistence
	clock              func() time.Time
	workers            sync.WaitGroup
	failedPersistences int
	stopped            bool
}

func newImpressionPreparationLifecycle(clock func() time.Time) *impressionPreparationLifecycle {
	return &impressionPreparationLifecycle{
		admission: make(chan struct{}, retainedImpressionPreparations),
		persistences: make(
			map[impressionIdentity]*impressionPersistence,
			retainedImpressionPreparations,
		),
		clock: clock,
	}
}

func (l *impressionPreparationLifecycle) prepareWithinBudget(
	ctx context.Context,
	prepare func() (impressionPreparation, error),
) (PreparedImpression, error) {
	if err := ctx.Err(); err != nil {
		return PreparedImpression{}, fmt.Errorf("prepare impression: %w", err)
	}
	if err := l.admit(); err != nil {
		return PreparedImpression{}, err
	}
	responseContext, cancel := context.WithTimeout(ctx, impressionPreparationBudget)
	defer cancel()
	planned := make(chan impressionPreparation)
	completed := make(chan impressionPreparationOutcome)
	abandoned := make(chan struct{})
	go l.runImpressionPreparation(impressionPreparationTask{
		responseContext:    responseContext,
		persistenceContext: context.WithoutCancel(ctx),
		prepare:            prepare,
		planned:            planned,
		completed:          completed,
		abandoned:          abandoned,
	})
	var prepared PreparedImpression
	for {
		select {
		case plan := <-planned:
			prepared = plan.prepared
			planned = nil
		case outcome := <-completed:
			return outcome.prepared, outcome.err
		case <-responseContext.Done():
			return resolveImpressionDeadline(
				responseContext,
				completed,
				abandoned,
				prepared,
			)
		}
	}
}

func resolveImpressionDeadline(
	responseContext context.Context,
	completed <-chan impressionPreparationOutcome,
	abandoned chan<- struct{},
	prepared PreparedImpression,
) (PreparedImpression, error) {
	select {
	case outcome := <-completed:
		return outcome.prepared, outcome.err
	default:
	}
	close(abandoned)
	if prepared.Token != "" {
		return prepared, nil
	}

	return PreparedImpression{}, fmt.Errorf(
		"prepare impression: %w",
		responseContext.Err(),
	)
}

func (l *impressionPreparationLifecycle) admit() error {
	l.state.Lock()
	defer l.state.Unlock()
	if l.stopped {
		return errImpressionPreparationStopped
	}
	l.pruneExpiredPersistencesLocked(l.clock().UTC())
	if l.failedPersistences+len(l.admission) >= maximumRetainedFailedImpressionPersistences {
		return errImpressionPersistenceUnavailable
	}
	select {
	case l.admission <- struct{}{}:
		l.workers.Add(1)

		return nil
	default:
		return errImpressionPreparationBusy
	}
}

func (l *impressionPreparationLifecycle) runImpressionPreparation(
	task impressionPreparationTask,
) {
	releaseAdmission := sync.OnceFunc(func() { <-l.admission })
	defer l.workers.Done()
	defer releaseAdmission()
	plan, err := task.prepare()
	if err != nil {
		releaseAdmission()
		select {
		case task.completed <- impressionPreparationOutcome{err: err}:
		case <-task.abandoned:
		}

		return
	}
	persistence := l.registerPersistence(plan.prepared.Token, plan.expires)
	select {
	case task.planned <- plan:
	case <-task.responseContext.Done():
		l.finishPersistence(
			plan.prepared.Token,
			persistence,
			task.responseContext.Err(),
			false,
		)

		return
	}
	err = plan.persist(task.persistenceContext)
	l.finishPersistence(plan.prepared.Token, persistence, err, true)
	releaseAdmission()
	select {
	case task.completed <- impressionPreparationOutcome{prepared: plan.prepared, err: err}:
	case <-task.abandoned:
		if err != nil {
			slog.WarnContext(
				task.persistenceContext,
				retainedImpressionPersistenceFailedText,
				slog.Any("error", err),
			)
		}
	}
}

func (l *impressionPreparationLifecycle) stop() {
	l.state.Lock()
	l.stopped = true
	l.state.Unlock()
	l.workers.Wait()
}
