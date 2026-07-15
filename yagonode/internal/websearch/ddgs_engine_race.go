package websearch

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"
)

const engineHedgeDelay = 50 * time.Millisecond

type engineAttempt struct {
	preference  int
	backend     engine
	results     []Result
	rateLimited bool
	err         error
}

type engineRace struct {
	provider *DDGSProvider
	ctx      context.Context
	query    providerQuery
	engines  []engine
	attempts chan engineAttempt
	launched int
	finished int
	errors   []error
	limited  []bool
}

func newEngineRace(
	provider *DDGSProvider,
	ctx context.Context,
	query providerQuery,
) *engineRace {
	engines := make([]engine, 0, len(provider.engines))
	for _, backend := range provider.engines {
		if !provider.backedOff(backend.name) {
			engines = append(engines, backend)
		}
	}

	return &engineRace{
		provider: provider,
		ctx:      ctx,
		query:    query,
		engines:  engines,
		attempts: make(chan engineAttempt, len(engines)),
		errors:   make([]error, len(engines)),
		limited:  make([]bool, len(engines)),
	}
}

func (r *engineRace) run() ([]Result, bool, error) {
	if len(r.engines) == 0 {
		return nil, true, nil
	}
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	r.ctx = ctx
	pendingLaunches := 1
	hedge := time.NewTimer(engineHedgeDelay)
	defer hedge.Stop()

	for r.finished < len(r.engines) {
		var admission chan struct{}
		if pendingLaunches > 0 {
			admission = r.provider.admission.slots
		}
		select {
		case <-r.ctx.Done():
			return nil, false, fmt.Errorf("web-search engine race: %w", r.ctx.Err())
		case admission <- struct{}{}:
			pendingLaunches--
			r.launchNext()
			resetHedge(hedge, r.launched+pendingLaunches < len(r.engines))
		case attempt := <-r.attempts:
			ready := r.drainReady(attempt)
			if results := r.evaluate(ready); len(results) > 0 {
				return results, false, nil
			}
			remaining := len(r.engines) - r.launched - pendingLaunches
			pendingLaunches += min(len(ready), remaining)
			resetHedge(hedge, r.launched+pendingLaunches < len(r.engines))
		case <-hedge.C:
			if r.launched+pendingLaunches < len(r.engines) {
				pendingLaunches++
			}
			resetHedge(hedge, r.launched+pendingLaunches < len(r.engines))
		}
	}

	allRateLimited := true
	var lastErr error
	for preference, err := range r.errors {
		if !r.limited[preference] {
			allRateLimited = false
		}
		if err != nil {
			lastErr = err
		}
	}
	if allRateLimited {
		return nil, true, nil
	}

	return nil, false, lastErr
}

func (r *engineRace) launchNext() {
	preference := r.launched
	backend := r.engines[preference]
	r.launched++
	go func() {
		attempt := func() engineAttempt {
			defer r.provider.admission.release()
			results, rateLimited, err := r.provider.fetch(
				r.ctx,
				backend,
				r.query.outboundText,
			)

			return engineAttempt{
				preference:  preference,
				backend:     backend,
				results:     results,
				rateLimited: rateLimited,
				err:         err,
			}
		}()
		select {
		case r.attempts <- attempt:
		case <-r.ctx.Done():
		}
	}()
}

func (r *engineRace) drainReady(first engineAttempt) []engineAttempt {
	ready := []engineAttempt{first}
	for {
		select {
		case attempt := <-r.attempts:
			ready = append(ready, attempt)
		default:
			slices.SortFunc(ready, func(left, right engineAttempt) int {
				return left.preference - right.preference
			})

			return ready
		}
	}
}

func (r *engineRace) evaluate(attempts []engineAttempt) []Result {
	for _, attempt := range attempts {
		r.finished++
		fetched := len(attempt.results)
		if attempt.err == nil && r.provider.accept != nil {
			attempt.results = r.provider.accept(r.query.submittedText, attempt.results)
		}
		slog.DebugContext(r.ctx, "web-search engine attempt",
			slog.String("engine", attempt.backend.name),
			slog.Int("fetched", fetched),
			slog.Int("accepted", len(attempt.results)),
			slog.Bool("rateLimited", attempt.rateLimited),
			slog.String("failure", webSearchFailureReason(attempt.err)))
		if attempt.rateLimited {
			r.provider.recordBackoff(attempt.backend.name)
			r.limited[attempt.preference] = true

			continue
		}
		r.provider.resetBackoff(attempt.backend.name)
		r.errors[attempt.preference] = attempt.err
		if attempt.err == nil && len(attempt.results) > 0 {
			return attempt.results
		}
	}

	return nil
}

func resetHedge(timer *time.Timer, pending bool) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	if pending {
		timer.Reset(engineHedgeDelay)
	}
}
