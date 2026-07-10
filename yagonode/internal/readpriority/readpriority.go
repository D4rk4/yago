// Package readpriority defers a bulk writer while interactive reads are in
// flight, so searches and admin pages reach the disk ahead of ingest writes and
// their fsyncs (IO-PRIO-01, made a configurable, observable gate for
// PERF-PRIO-02). The wait is bounded by a budget: a read-heavy node delays each
// write by at most the budget, so writers never starve, and a node with no
// concurrent reads pays nothing.
package readpriority

import (
	"context"
	"time"
)

// DefaultBudget preserves the original inline IO-PRIO-01 yield ceiling, so a
// node that does not tune the budget behaves exactly as it did before the yield
// became a configurable, observable component.
const DefaultBudget = 50 * time.Millisecond

// step is the poll interval between read-activity checks while deferring.
const step = 2 * time.Millisecond

// sleepFor is a seam so tests advance the deferral without real waiting.
var sleepFor = time.Sleep

// Await blocks while readsBusy reports interactive reads are in flight, up to
// budget, and returns how long it deferred the caller. It defers nothing —
// returning at once — when budget is non-positive, readsBusy is nil, no reads
// are in flight, or ctx is cancelled, so a writer is never starved and an idle
// node pays nothing.
func Await(ctx context.Context, budget time.Duration, readsBusy func() bool) time.Duration {
	if budget <= 0 || readsBusy == nil {
		return 0
	}
	waited := time.Duration(0)
	for waited < budget {
		if !readsBusy() || ctx.Err() != nil {
			break
		}
		sleepFor(step)
		waited += step
	}

	return waited
}
