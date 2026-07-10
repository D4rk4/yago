package shardvault

import "time"

// SetReadDeferBudget sets how long an Update yields to in-flight interactive
// reads (IO-PRIO-01 / PERF-PRIO-02). The node calls it once at boot with the
// operator's YAGO_STORAGE_READ_DEFER setting: zero keeps the default ceiling, a
// larger budget prioritizes reads harder under sustained load, and a negative
// value disables the yield.
func (e *engine) SetReadDeferBudget(budget time.Duration) {
	e.readDeferBudget.Store(int64(budget))
}

// ReadDeferred reports the cumulative time Updates have yielded to in-flight
// interactive reads — the saturation signal for read-vs-ingest contention
// (PERF-PRIO-02).
func (e *engine) ReadDeferred() time.Duration {
	return time.Duration(e.readDeferNanos.Load())
}
