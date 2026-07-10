package shardvault

import (
	"testing"
	"time"
)

// TestReadDeferBudgetAndAccounting: the read-defer budget is a live setting the
// node applies at boot, and ReadDeferred exposes the cumulative time writes have
// yielded to reads for the saturation metric (PERF-PRIO-02).
func TestReadDeferBudgetAndAccounting(t *testing.T) {
	e := openTestEngine(t)
	if got := e.ReadDeferred(); got != 0 {
		t.Fatalf("fresh engine ReadDeferred = %v, want 0", got)
	}

	e.SetReadDeferBudget(250 * time.Millisecond)
	if got := time.Duration(e.readDeferBudget.Load()); got != 250*time.Millisecond {
		t.Fatalf("read-defer budget = %v, want 250ms", got)
	}

	e.readDeferNanos.Add(int64(3 * time.Millisecond))
	if got := e.ReadDeferred(); got != 3*time.Millisecond {
		t.Fatalf("ReadDeferred = %v, want 3ms", got)
	}
}
