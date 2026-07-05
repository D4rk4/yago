package yagonode

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
)

var (
	errUnknownCrawlRun    = errors.New("unknown crawl run")
	errUnknownCrawlAction = errors.New("unknown crawl control action")
)

// crawlControlRegistry returns the broker's control registry when the runtime is a
// live crawl runtime, or nil when crawling is disabled (or the runtime is a test
// double), so control actions are wired only when there is a fleet to steer.
func crawlControlRegistry(runtime crawlProcess) *crawlbroker.ControlRegistry {
	provider, ok := runtime.(interface {
		controlRegistry() *crawlbroker.ControlRegistry
	})
	if !ok {
		return nil
	}

	return provider.controlRegistry()
}

// crawlControlSource turns an operator control request into a directive queued for
// the worker that runs the target run. It resolves the run's worker through the
// run registry and enqueues on the broker's control registry.
type crawlControlSource struct {
	runs      *crawlruns.Registry
	control   *crawlbroker.ControlRegistry
	restarter crawlRestarter
}

// crawlRestartSource wraps a dispatcher as a restarter, keeping a nil
// dispatcher a nil interface so the restart action reports unavailable
// instead of panicking through a typed-nil method call.
func crawlRestartSource(dispatcher *crawldispatch.Dispatcher) crawlRestarter {
	if dispatcher == nil {
		return nil
	}

	return dispatcher
}

// crawlRestarter re-dispatches the last order seen for a profile handle.
type crawlRestarter interface {
	Restart(ctx context.Context, profileHandle string) (crawldispatch.Accepted, error)
}

func newCrawlControlSource(
	runs *crawlruns.Registry,
	control *crawlbroker.ControlRegistry,
	restarter crawlRestarter,
) *crawlControlSource {
	return &crawlControlSource{runs: runs, control: control, restarter: restarter}
}

// Control enqueues a control directive for the worker running the requested run. A
// run whose worker is unknown, or an unsupported action, is rejected without
// enqueuing anything.
func (s *crawlControlSource) Control(ctx context.Context, req adminui.CrawlControlRequest) error {
	if req.Action == "restart" {
		return s.restart(ctx, req.RunID)
	}
	kind, ok := crawlControlKind(req.Action)
	if !ok {
		return fmt.Errorf("%w: %q", errUnknownCrawlAction, req.Action)
	}
	worker, ok := s.workerForRun(req.RunID)
	if !ok {
		return fmt.Errorf("%w: %q", errUnknownCrawlRun, req.RunID)
	}

	s.control.Enqueue(worker, yagocrawlcontract.CrawlControlDirective{
		Kind:           kind,
		RunID:          req.RunID,
		PagesPerMinute: req.PagesPerMinute,
	})
	if kind == yagocrawlcontract.CrawlControlSetRate {
		s.runs.SetRate(req.RunID, req.PagesPerMinute)
	}

	return nil
}

// restart resolves the run's profile handle and re-dispatches its last order
// as a brand-new run.
func (s *crawlControlSource) restart(ctx context.Context, runID string) error {
	if s.restarter == nil {
		return fmt.Errorf("%w: restart", errUnknownCrawlAction)
	}
	handle, ok := s.profileForRun(runID)
	if !ok {
		return fmt.Errorf("%w: %q", errUnknownCrawlRun, runID)
	}
	if _, err := s.restarter.Restart(ctx, handle); err != nil {
		return fmt.Errorf("restart crawl %s: %w", handle, err)
	}

	return nil
}

func (s *crawlControlSource) profileForRun(runID string) (string, bool) {
	if runID == "" {
		return "", false
	}
	for _, run := range s.runs.Recent() {
		if run.RunID == runID && run.ProfileHandle != "" {
			return run.ProfileHandle, true
		}
	}

	return "", false
}

func (s *crawlControlSource) workerForRun(runID string) (string, bool) {
	if runID == "" {
		return "", false
	}
	for _, run := range s.runs.Recent() {
		if run.RunID == runID && run.WorkerID != "" {
			return run.WorkerID, true
		}
	}

	return "", false
}

func crawlControlKind(action string) (yagocrawlcontract.CrawlControlKind, bool) {
	switch action {
	case "pause":
		return yagocrawlcontract.CrawlControlPause, true
	case "resume":
		return yagocrawlcontract.CrawlControlResume, true
	case "cancel":
		return yagocrawlcontract.CrawlControlCancel, true
	case "set_rate":
		return yagocrawlcontract.CrawlControlSetRate, true
	default:
		return "", false
	}
}
