package crawlbroker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
)

func TestPersistentControlDirectiveFollowsRunLifecycle(t *testing.T) {
	const runID = "61646d696e"
	path := filepath.Join(t.TempDir(), "node.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	registry, err := newPersistentControlRegistry(storage)
	if err != nil {
		t.Fatalf("open control registry: %v", err)
	}
	queue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open order queue: %v", err)
	}
	registry.register("old-worker")
	if !registry.Enqueue("old-worker", yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlCancel,
		RunID: runID,
	}) {
		t.Fatal("enqueue run directive")
	}
	if !registry.Enqueue("old-worker", yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlPause,
		RunID: "cd",
	}) {
		t.Fatal("enqueue other run directive")
	}
	leaseOne(t, queue, "owned", "new-worker")
	if _, err := registry.ReassignRunIfLeaseOwned(
		t.Context(),
		queue,
		"new-worker",
		runID,
	); err != nil {
		t.Fatalf("move run directive: %v", err)
	}
	if old := deliveredControls(t, registry, "old-worker"); len(old) != 1 || old[0].RunID != "cd" {
		t.Fatalf("old worker directives = %+v, want only other run", old)
	}
	moved := deliveredControls(t, registry, "new-worker")
	if len(moved) != 1 || moved[0].RunID != runID {
		t.Fatalf("new worker directives = %+v, want moved cancellation", moved)
	}
	if err := registry.CompleteRun(t.Context(), leaseControlTarget{
		WorkerID: "new-worker",
		RunID:    runID,
	}); err != nil {
		t.Fatalf("finish run directives: %v", err)
	}
	if remaining := deliveredControls(t, registry, "new-worker"); len(remaining) != 0 {
		t.Fatalf("terminal run directives = %+v, want empty", remaining)
	}
	if err := registry.CompleteRun(t.Context(), leaseControlTarget{
		WorkerID: "old-worker",
		RunID:    "cd",
	}); err != nil {
		t.Fatalf("cancel other run directives: %v", err)
	}
	if err := registry.CompleteRun(t.Context(), leaseControlTarget{}); err != nil {
		t.Fatalf("empty run reconciliation: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}

	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	registry, err = newPersistentControlRegistry(storage)
	if err != nil {
		t.Fatalf("reopen control registry: %v", err)
	}
	if remaining := deliveredControls(t, registry, "new-worker"); len(remaining) != 0 {
		t.Fatalf("reopened terminal directives = %+v, want empty", remaining)
	}
}

func TestMemoryControlDirectiveLifecycleIgnoresOtherRuns(t *testing.T) {
	ledger := newMemoryControlDirectiveLedger()
	directive, err := ledger.Enqueue(
		context.Background(),
		"old",
		yagocrawlcontract.CrawlControlDirective{
			Kind:  yagocrawlcontract.CrawlControlPause,
			RunID: "ab",
		},
	)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := ledger.ReconcileRun(t.Context(), "new", "cd", false); err != nil {
		t.Fatalf("reconcile other run: %v", err)
	}
	if err := ledger.ReconcileRun(t.Context(), "new", "", true); err != nil {
		t.Fatalf("reconcile empty run: %v", err)
	}
	old, err := ledger.Exchange(t.Context(), "old", nil)
	if err != nil || len(old) != 1 || old[0] != directive {
		t.Fatalf("old directives = %+v err=%v, want unchanged", old, err)
	}
	if err := ledger.ReconcileRun(t.Context(), "new", "ab", false); err != nil {
		t.Fatalf("move run: %v", err)
	}
	if err := ledger.ReconcileRun(t.Context(), "new", "ab", true); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	remaining, err := ledger.Exchange(t.Context(), "new", nil)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining directives = %+v err=%v, want empty", remaining, err)
	}
}
