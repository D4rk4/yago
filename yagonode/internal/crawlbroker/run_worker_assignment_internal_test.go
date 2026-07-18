package crawlbroker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type observedRunReconciliation struct {
	workerID string
	runID    string
	terminal bool
}

type observedRunDirective struct {
	workerID  string
	directive yagocrawlcontract.CrawlControlDirective
}

type observedRunReconciliationLedger struct {
	mu              sync.Mutex
	reconciliations []observedRunReconciliation
	directives      []observedRunDirective
	reconcileErr    error
}

func (l *observedRunReconciliationLedger) Enqueue(
	_ context.Context,
	workerID string,
	directive yagocrawlcontract.CrawlControlDirective,
) (yagocrawlcontract.CrawlControlDirective, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.directives = append(l.directives, observedRunDirective{
		workerID:  workerID,
		directive: directive,
	})

	return directive, nil
}

func (l *observedRunReconciliationLedger) Exchange(
	_ context.Context,
	_ string,
	_ []uint64,
) ([]yagocrawlcontract.CrawlControlDirective, error) {
	return nil, nil
}

func (l *observedRunReconciliationLedger) ReconcileRun(
	_ context.Context,
	workerID string,
	runID string,
	terminal bool,
) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reconciliations = append(l.reconciliations, observedRunReconciliation{
		workerID: workerID,
		runID:    runID,
		terminal: terminal,
	})

	return l.reconcileErr
}

func (l *observedRunReconciliationLedger) setReconcileError(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reconcileErr = err
}

func (l *observedRunReconciliationLedger) observedReconciliations() []observedRunReconciliation {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]observedRunReconciliation(nil), l.reconciliations...)
}

func (l *observedRunReconciliationLedger) observedDirectives() []observedRunDirective {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]observedRunDirective(nil), l.directives...)
}

func TestAuthorizedRunningProgressReconcilesEachRunOnce(t *testing.T) {
	const runTotal = 270
	ledger := &observedRunReconciliationLedger{}
	server := &exchangeServer{
		control:  newControlRegistryWithLedger(ledger),
		progress: noopProgressSink{},
	}
	for run := 1; run <= runTotal; run++ {
		runID := fmt.Sprintf("%08x", run)
		progress := yagocrawlcontract.CrawlRunProgress{
			RunID:    runID,
			WorkerID: "worker",
			State:    yagocrawlcontract.CrawlRunRunning,
		}
		for report := 0; report < 5; report++ {
			if err := server.recordAuthorizedProgress(t.Context(), progress); err != nil {
				t.Fatalf("record run %d progress %d: %v", run, report, err)
			}
		}
	}
	reconciliations := ledger.observedReconciliations()
	if len(reconciliations) != runTotal {
		t.Fatalf("run reconciliations = %d, want %d", len(reconciliations), runTotal)
	}
	for index, reconciliation := range reconciliations {
		wantedRunID := fmt.Sprintf("%08x", index+1)
		if reconciliation.workerID != "worker" || reconciliation.runID != wantedRunID ||
			reconciliation.terminal {
			t.Fatalf("run reconciliation %d = %+v", index, reconciliation)
		}
	}
}

func TestControlRegistrySerializesFirstRunAssignment(t *testing.T) {
	const reporterTotal = 64
	ledger := &observedRunReconciliationLedger{}
	registry := newControlRegistryWithLedger(ledger)
	start := make(chan struct{})
	errs := make(chan error, reporterTotal)
	var reporters sync.WaitGroup
	for range reporterTotal {
		reporters.Add(1)
		go func() {
			defer reporters.Done()
			<-start
			errs <- registry.reassignAuthorizedRun(t.Context(), "worker", "run")
		}()
	}
	close(start)
	reporters.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent run assignment: %v", err)
		}
	}
	if reconciliations := ledger.observedReconciliations(); len(reconciliations) != 1 {
		t.Fatalf("concurrent run reconciliations = %+v, want one", reconciliations)
	}
}

func TestControlRegistryRoutesRunDirectiveToCachedWorker(t *testing.T) {
	ledger := &observedRunReconciliationLedger{}
	registry := newControlRegistryWithLedger(ledger)
	runID := "72756e"
	if err := registry.reassignAuthorizedRun(t.Context(), "worker-b", runID); err != nil {
		t.Fatalf("assign current run worker: %v", err)
	}
	directive := yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlPause,
		RunID: runID,
	}
	if !registry.Enqueue("worker-a", directive) {
		t.Fatal("enqueue run directive through stale worker")
	}
	directives := ledger.observedDirectives()
	if len(directives) != 1 {
		t.Fatalf("enqueued run directives = %+v, want one", directives)
	}
	if directives[0].workerID != "worker-b" || directives[0].directive != directive {
		t.Fatalf("enqueued run directive = %+v, want worker-b %+v", directives[0], directive)
	}
}

func TestControlRegistryRetriesWorkerMigrationAndCompletion(t *testing.T) {
	ledger := &observedRunReconciliationLedger{}
	registry := newControlRegistryWithLedger(ledger)
	if err := registry.reassignAuthorizedRun(t.Context(), "worker-a", "run"); err != nil {
		t.Fatalf("assign first worker: %v", err)
	}
	wantedErr := errors.New("reconcile failed")
	ledger.setReconcileError(wantedErr)
	if err := registry.reassignAuthorizedRun(
		t.Context(),
		"worker-b",
		"run",
	); !errors.Is(
		err,
		wantedErr,
	) {
		t.Fatalf("failed worker migration error = %v", err)
	}
	if err := registry.reassignAuthorizedRun(t.Context(), "worker-a", "run"); err != nil {
		t.Fatalf("retain first worker after failed migration: %v", err)
	}
	if reconciliations := ledger.observedReconciliations(); len(reconciliations) != 2 {
		t.Fatalf("failed migration reconciliations = %+v, want two", reconciliations)
	}
	ledger.setReconcileError(nil)
	if err := registry.reassignAuthorizedRun(t.Context(), "worker-b", "run"); err != nil {
		t.Fatalf("retry worker migration: %v", err)
	}
	if err := registry.reassignAuthorizedRun(t.Context(), "worker-b", "run"); err != nil {
		t.Fatalf("repeat migrated worker: %v", err)
	}
	ledger.setReconcileError(wantedErr)
	target := leaseControlTarget{WorkerID: "worker-b", RunID: "run"}
	if err := registry.CompleteRun(t.Context(), target); !errors.Is(err, wantedErr) {
		t.Fatalf("failed completion error = %v", err)
	}
	if err := registry.reassignAuthorizedRun(t.Context(), "worker-b", "run"); err != nil {
		t.Fatalf("retain assignment after failed completion: %v", err)
	}
	ledger.setReconcileError(nil)
	if err := registry.CompleteRun(t.Context(), target); err != nil {
		t.Fatalf("retry completion: %v", err)
	}
	if err := registry.reassignAuthorizedRun(t.Context(), "worker-b", "run"); err != nil {
		t.Fatalf("assign completed run again: %v", err)
	}
	reconciliations := ledger.observedReconciliations()
	if len(reconciliations) != 6 {
		t.Fatalf("migration and completion reconciliations = %+v, want six", reconciliations)
	}
	wanted := []observedRunReconciliation{
		{workerID: "worker-a", runID: "run"},
		{workerID: "worker-b", runID: "run"},
		{workerID: "worker-b", runID: "run"},
		{workerID: "worker-b", runID: "run", terminal: true},
		{workerID: "worker-b", runID: "run", terminal: true},
		{workerID: "worker-b", runID: "run"},
	}
	for index := range wanted {
		if reconciliations[index] != wanted[index] {
			t.Fatalf(
				"reconciliation %d = %+v, want %+v",
				index,
				reconciliations[index],
				wanted[index],
			)
		}
	}
}
