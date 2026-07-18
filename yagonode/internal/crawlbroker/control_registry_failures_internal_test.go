package crawlbroker

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type scriptedControlDirectiveLedger struct {
	pending      []yagocrawlcontract.CrawlControlDirective
	enqueueErr   error
	exchangeErr  error
	reconcileErr error
}

func (l *scriptedControlDirectiveLedger) ReconcileRun(
	_ context.Context,
	_ string,
	_ string,
	_ bool,
) error {
	return l.reconcileErr
}

func (l *scriptedControlDirectiveLedger) Enqueue(
	_ context.Context,
	_ string,
	directive yagocrawlcontract.CrawlControlDirective,
) (yagocrawlcontract.CrawlControlDirective, error) {
	if l.enqueueErr != nil {
		return yagocrawlcontract.CrawlControlDirective{}, l.enqueueErr
	}
	directive.DirectiveID = uint64(len(l.pending) + 1)
	l.pending = append(l.pending, directive)

	return directive, nil
}

func (l *scriptedControlDirectiveLedger) Exchange(
	_ context.Context,
	_ string,
	_ []uint64,
) ([]yagocrawlcontract.CrawlControlDirective, error) {
	if l.exchangeErr != nil {
		return nil, l.exchangeErr
	}

	return append([]yagocrawlcontract.CrawlControlDirective(nil), l.pending...), nil
}

func TestControlRegistrySurfacesInitializationFailure(t *testing.T) {
	ledger := &scriptedControlDirectiveLedger{exchangeErr: errors.New("read failed")}
	registry := newControlRegistryWithLedger(ledger, crawlerControlDefaults{fetchWorkers: 4})
	registry.register("worker")
	if _, err := registry.deliverForHeartbeat(t.Context(), "worker", nil); err == nil {
		t.Fatal("expected initialization failure")
	}
}

func TestControlRegistryKeepsMatchingInitialDirective(t *testing.T) {
	wanted := yagocrawlcontract.CrawlControlDirective{
		DirectiveID:  8,
		Kind:         yagocrawlcontract.CrawlControlSetWorkers,
		FetchWorkers: 4,
	}
	ledger := &scriptedControlDirectiveLedger{
		pending: []yagocrawlcontract.CrawlControlDirective{wanted},
	}
	registry := newControlRegistryWithLedger(ledger, crawlerControlDefaults{fetchWorkers: 4})
	directives, err := registry.deliverForHeartbeat(t.Context(), "worker", nil)
	if err != nil || len(directives) != 2 || directives[0] != wanted ||
		directives[1].Kind != yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority {
		t.Fatalf(
			"directives = %+v err=%v, want retained workers and appended priority",
			directives,
			err,
		)
	}
}

func TestControlRegistrySurfacesInitialEnqueueFailure(t *testing.T) {
	ledger := &scriptedControlDirectiveLedger{enqueueErr: errors.New("write failed")}
	registry := newControlRegistryWithLedger(ledger, crawlerControlDefaults{fetchWorkers: 4})
	if _, err := registry.deliverForHeartbeat(t.Context(), "worker", nil); err == nil {
		t.Fatal("expected initial enqueue failure")
	}
}

func TestControlRegistryReportsBroadcastStorageFailures(t *testing.T) {
	ledger := &scriptedControlDirectiveLedger{}
	registry := newControlRegistryWithLedger(ledger)
	registry.register("worker")
	ledger.enqueueErr = errors.New("write failed")
	if signalled := registry.SetFetchWorkers(4); signalled != 0 {
		t.Fatalf("fetch worker failures signalled = %d, want 0", signalled)
	}
	if registry.initialized["worker"] {
		t.Fatal("fetch worker failure retained initialized state")
	}
	registry.initialized["worker"] = true
	if signalled := registry.SetAutomaticDiscoveryPriority(true); signalled != 0 {
		t.Fatalf("priority failures signalled = %d, want 0", signalled)
	}
	if registry.initialized["worker"] {
		t.Fatal("priority failure retained initialized state")
	}
}

func TestExchangeHeartbeatSurfacesControlStorageFailure(t *testing.T) {
	ledger := &scriptedControlDirectiveLedger{exchangeErr: errors.New("read failed")}
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	server.control = newControlRegistryWithLedger(ledger)
	server.control.initialized["worker"] = true
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	if _, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
	}); err == nil {
		t.Fatal("expected heartbeat control storage failure")
	}
}

func TestExchangeProgressSurfacesControlReconciliationFailure(t *testing.T) {
	ledger := &scriptedControlDirectiveLedger{reconcileErr: errors.New("write failed")}
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "progress", "worker", testWorkerSessionID)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	server.control = newControlRegistryWithLedger(ledger)
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	if _, err := server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
		LeaseId:         leaseID,
		WorkerId:        "worker",
		WorkerSessionId: testWorkerSessionID,
		RunId:           []byte("admin"),
		State:           crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
	}); err == nil {
		t.Fatal("expected progress control reconciliation failure")
	}
}
