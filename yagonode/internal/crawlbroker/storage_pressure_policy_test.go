package crawlbroker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type scriptedGrowthAdmission struct {
	err   error
	calls int
}

type blockingGrowthAdmission struct {
	started chan struct{}
	release chan struct{}
	err     error
}

type cancelingGrowthAdmission struct {
	cancel func()
	err    error
}

type mutatingGrowthAdmission struct {
	mutate func()
}

func (a mutatingGrowthAdmission) CheckGrowth() error {
	a.mutate()

	return nil
}

func (a cancelingGrowthAdmission) CheckGrowth() error {
	a.cancel()

	return a.err
}

func (a blockingGrowthAdmission) CheckGrowth() error {
	close(a.started)
	<-a.release

	return a.err
}

func (a *scriptedGrowthAdmission) CheckGrowth() error {
	a.calls++

	return a.err
}

func TestCrawlOrderGrowthAdmissionPreservesDuplicatesAndRecoveryWrites(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	pressure := &scriptedGrowthAdmission{err: errors.New("pressure")}
	queue, err := newDurableOrderQueue(storage, DefaultLeaseTTL, pressure)
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	if _, err := queue.PublishOnce(t.Context(), "fresh", testOrder("blocked")); err == nil {
		t.Fatal("fresh order admitted under pressure")
	}
	pressure.err = nil
	duplicate, err := queue.PublishOnce(t.Context(), "accepted", testOrder("accepted"))
	if err != nil || duplicate {
		t.Fatalf("accepted order duplicate=%v error=%v", duplicate, err)
	}
	pressure.err = errors.New("pressure")
	duplicate, err = queue.PublishOnce(t.Context(), "accepted", testOrder("retry"))
	if err != nil || !duplicate {
		t.Fatalf("duplicate under pressure duplicate=%v error=%v", duplicate, err)
	}
	if pressure.calls != 2 {
		t.Fatalf("growth checks = %d, want 2", pressure.calls)
	}
	data, err := marshalCrawlOrder(testOrder("recovered"))
	if err != nil {
		t.Fatalf("marshal recovered order: %v", err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := queue.enqueueTx(tx, data, yagocrawlcontract.CrawlOrderPriorityNormal)

		return err
	}); err != nil {
		t.Fatalf("recovery enqueue under pressure: %v", err)
	}
}

func TestCrawlOrderGrowthAdmissionDoesNotHoldWriterAndPreservesConcurrentDuplicate(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	admission := blockingGrowthAdmission{
		started: make(chan struct{}),
		release: make(chan struct{}),
		err:     errors.New("pressure"),
	}
	queue, err := newDurableOrderQueue(storage, DefaultLeaseTTL, admission)
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	type publishResult struct {
		duplicate bool
		err       error
	}
	published := make(chan publishResult, 1)
	go func() {
		duplicate, err := queue.PublishOnce(t.Context(), "same", testOrder("same"))
		published <- publishResult{duplicate: duplicate, err: err}
	}()
	<-admission.started
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return queue.keys.Put(tx, vault.Key("same"), 7)
		})
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("record concurrent duplicate: %v", err)
		}
	case <-time.After(time.Second):
		close(admission.release)
		t.Fatal("growth measurement held the crawl-order writer transaction")
	}
	close(admission.release)
	result := <-published
	if result.err != nil || !result.duplicate {
		t.Fatalf("concurrent publish duplicate=%t error=%v", result.duplicate, result.err)
	}
}

func TestCrawlOrderGrowthAdmissionReturnsSecondIdempotencyReadFailure(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	queue, err := newDurableOrderQueue(
		storage,
		DefaultLeaseTTL,
		cancelingGrowthAdmission{cancel: cancel, err: errors.New("pressure")},
	)
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	if _, err := queue.admitOrderGrowth(ctx, "fresh"); !errors.Is(err, context.Canceled) {
		t.Fatalf("second idempotency read error = %v", err)
	}
}

func TestCrawlOrderPublishPreservesConcurrentDuplicateAfterSuccessfulAdmission(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	admission := blockingGrowthAdmission{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	queue, err := newDurableOrderQueue(storage, DefaultLeaseTTL, admission)
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	type publishResult struct {
		duplicate bool
		err       error
	}
	published := make(chan publishResult, 1)
	go func() {
		duplicate, publishErr := queue.PublishOnce(t.Context(), "same", testOrder("same"))
		published <- publishResult{duplicate: duplicate, err: publishErr}
	}()
	<-admission.started
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return queue.keys.Put(tx, vault.Key("same"), 7)
	}); err != nil {
		close(admission.release)
		t.Fatalf("record concurrent duplicate: %v", err)
	}
	close(admission.release)
	result := <-published
	if result.err != nil || !result.duplicate {
		t.Fatalf("concurrent publish duplicate=%t error=%v", result.duplicate, result.err)
	}
}

func TestCrawlOrderPublishReportsConcurrentMalformedIdempotencyRecord(t *testing.T) {
	fixture := scriptedQueue(t)
	fixture.queue.growthAdmission = mutatingGrowthAdmission{mutate: func() {
		fixture.engine.buckets[idempotencyBucket]["same"] = []byte{1}
	}}
	if _, err := fixture.queue.PublishOnce(t.Context(), "same", testOrder("same")); err == nil {
		t.Fatal("concurrent malformed idempotency record was accepted")
	}
}

func TestCrawlerStoragePressurePolicyAndReports(t *testing.T) {
	registry := newControlRegistry(crawlerControlDefaults{
		fetchWorkers: 2,
		storagePressurePolicy: yagocrawlcontract.StoragePressurePolicy{
			ReservedFreeBytes: 100, RecoveryHysteresisBytes: 20,
		},
	})
	if got := registry.StoragePressurePolicy(); got.ReservedFreeBytes != 100 {
		t.Fatalf("initial policy = %+v", got)
	}
	registry.SetStoragePressurePolicy(yagocrawlcontract.StoragePressurePolicy{
		ReservedFreeBytes: 80, RecoveryHysteresisBytes: 10,
	})
	registry.recordStoragePressure("offline", storageHeartbeat(70, true, true))
	registry.register("one")
	registry.register("two")
	registry.recordStoragePressure("one", storageHeartbeat(70, true, true))
	registry.recordStoragePressure("two", storageHeartbeat(60, false, true))
	snapshot := registry.RuntimeSnapshot()
	if !snapshot.StorageStatesKnown || snapshot.StorageReportedCrawlers != 2 ||
		snapshot.StorageUnreportedCrawlers != 0 || snapshot.StoragePressured != 2 ||
		snapshot.StorageMeasurementsUnavailable != 1 ||
		snapshot.MinimumStorageAvailableBytes != 70 ||
		snapshot.StoragePressurePolicy.ReservedFreeBytes != 80 {
		t.Fatalf("storage runtime snapshot = %+v", snapshot)
	}
	registry.recordStoragePressure("two", &crawlrpc.WorkerHeartbeat{})
	mixed := registry.RuntimeSnapshot()
	if mixed.StorageStatesKnown || mixed.StorageReportedCrawlers != 1 ||
		mixed.StorageUnreportedCrawlers != 1 || mixed.StoragePressured != 1 ||
		mixed.MinimumStorageAvailableBytes != 70 {
		t.Fatalf("mixed crawler storage snapshot = %+v", mixed)
	}
	registry.unregister("one")
	if _, found := registry.storageStates["one"]; found {
		t.Fatal("disconnected crawler storage state retained")
	}
}

func TestCrawlerStorageReportsExpireWhileStreamRemainsConnected(t *testing.T) {
	registry := newControlRegistry()
	base := time.Unix(2_000_000, 0)
	now := base
	registry.now = func() time.Time { return now }
	registry.register("current")
	registry.register("legacy")
	registry.recordStoragePressure("current", storageHeartbeat(70, true, true))
	fresh := registry.RuntimeSnapshot()
	if fresh.StorageReportedCrawlers != 1 || fresh.StorageUnreportedCrawlers != 1 ||
		fresh.StoragePressured != 1 {
		t.Fatalf("fresh mixed report = %+v", fresh)
	}
	now = base.Add(crawlerHeartbeatReportLifetime)
	atBoundary := registry.RuntimeSnapshot()
	if atBoundary.StorageReportedCrawlers != 1 || atBoundary.StorageUnreportedCrawlers != 1 {
		t.Fatalf("boundary mixed report = %+v", atBoundary)
	}
	now = now.Add(time.Nanosecond)
	stale := registry.RuntimeSnapshot()
	if stale.StorageReportedCrawlers != 0 || stale.StorageUnreportedCrawlers != 2 ||
		stale.StoragePressured != 0 || stale.StorageStatesKnown {
		t.Fatalf("stale mixed report = %+v", stale)
	}
	registry.recordStoragePressure("current", storageHeartbeat(80, true, false))
	recovered := registry.RuntimeSnapshot()
	if recovered.StorageReportedCrawlers != 1 || recovered.StorageUnreportedCrawlers != 1 ||
		recovered.StoragePressured != 0 {
		t.Fatalf("recovered mixed report = %+v", recovered)
	}
}

func storageHeartbeat(
	available uint64,
	measurementAvailable bool,
	pressured bool,
) *crawlrpc.WorkerHeartbeat {
	return &crawlrpc.WorkerHeartbeat{
		StorageAvailableBytes:       &available,
		StorageMeasurementAvailable: &measurementAvailable,
		StoragePressure:             &pressured,
	}
}

func TestHeartbeatReturnsExplicitCrawlerStoragePolicy(t *testing.T) {
	server := newExchangeServer(
		memQueue(t),
		make(chan crawlresults.IngestDelivery),
		crawlerControlDefaults{storagePressurePolicy: yagocrawlcontract.StoragePressurePolicy{
			ReservedFreeBytes: 55, RecoveryHysteresisBytes: 7,
		}},
	)
	server.control.register("worker")
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	result, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
	})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if result.StorageReservedFreeBytes == nil ||
		result.StoragePressureHysteresisBytes == nil ||
		result.GetStorageReservedFreeBytes() != 55 ||
		result.GetStoragePressureHysteresisBytes() != 7 {
		t.Fatalf("heartbeat policy = %+v", result)
	}
}
