package eviction_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/eviction"
)

type scriptedSweeper struct {
	result eviction.Result
	err    error
}

func (s scriptedSweeper) Sweep(context.Context) (eviction.Result, error) {
	return s.result, s.err
}

type recordingObserver struct {
	observed []eviction.Result
	failures int
}

func (r *recordingObserver) Observe(result eviction.Result) {
	r.observed = append(r.observed, result)
}

func (r *recordingObserver) ObserveFailure() { r.failures++ }

func TestRunSweepLoopObservesDeletions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observer := &recordingObserver{}

	eviction.RunSweepLoop(ctx, scriptedSweeper{
		result: eviction.Result{URLsDeleted: 2, PostingsDeleted: 3},
	}, observer, time.Minute)

	if len(observer.observed) != 1 || observer.observed[0].URLsDeleted != 2 {
		t.Fatalf("observed = %+v", observer.observed)
	}
	if observer.failures != 0 {
		t.Fatalf("failures = %d, want 0", observer.failures)
	}
}

func TestRunSweepLoopObservesFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observer := &recordingObserver{}

	eviction.RunSweepLoop(ctx, scriptedSweeper{err: errors.New("boom")}, observer, time.Minute)

	if observer.failures != 1 {
		t.Fatalf("failures = %d, want 1", observer.failures)
	}
	if len(observer.observed) != 0 {
		t.Fatalf("observed = %+v, want none", observer.observed)
	}
}
