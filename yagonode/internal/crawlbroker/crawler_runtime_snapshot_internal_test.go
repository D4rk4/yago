package crawlbroker

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

func activeFetchReport(value uint32) *uint32 { return &value }

func TestCrawlerRuntimeSnapshotTracksUniqueConnectedWorkers(t *testing.T) {
	registry := newControlRegistry(crawlerControlDefaults{fetchWorkers: 4})
	if got := registry.RuntimeSnapshot(); got != (CrawlerRuntimeSnapshot{
		ActiveFetchesKnown:   true,
		FetchLimitPerCrawler: 4,
	}) {
		t.Fatalf("empty runtime snapshot = %+v", got)
	}

	registry.register("worker-1")
	registry.register("worker-1")
	registry.recordActiveFetches("worker-1", activeFetchReport(0))
	registry.register("worker-2")
	registry.recordActiveFetches("worker-2", activeFetchReport(3))
	if got := registry.RuntimeSnapshot(); got != (CrawlerRuntimeSnapshot{
		ConnectedCrawlers:         2,
		ActiveFetches:             3,
		ActiveFetchesKnown:        true,
		FetchLimitPerCrawler:      4,
		AggregateFetchCapacity:    8,
		StorageUnreportedCrawlers: 2,
	}) {
		t.Fatalf("connected runtime snapshot = %+v", got)
	}

	registry.unregister("worker-1")
	if got := registry.RuntimeSnapshot(); got.ConnectedCrawlers != 2 || got.ActiveFetches != 3 {
		t.Fatalf("overlapping stream snapshot = %+v", got)
	}
	registry.unregister("worker-1")
	if got := registry.RuntimeSnapshot(); got != (CrawlerRuntimeSnapshot{
		ConnectedCrawlers:         1,
		ActiveFetches:             3,
		ActiveFetchesKnown:        true,
		FetchLimitPerCrawler:      4,
		AggregateFetchCapacity:    4,
		StorageUnreportedCrawlers: 1,
	}) {
		t.Fatalf("disconnected runtime snapshot = %+v", got)
	}
}

func TestCrawlerRuntimeSnapshotDistinguishesLegacyAndZeroReports(t *testing.T) {
	registry := newControlRegistry(crawlerControlDefaults{fetchWorkers: 4})
	registry.register("current")
	registry.register("legacy")
	registry.recordActiveFetches("current", activeFetchReport(0))
	if got := registry.RuntimeSnapshot(); got.ActiveFetchesKnown {
		t.Fatalf("mixed fleet snapshot = %+v, want unknown activity", got)
	}

	registry.recordActiveFetches("legacy", activeFetchReport(2))
	if got := registry.RuntimeSnapshot(); !got.ActiveFetchesKnown || got.ActiveFetches != 2 {
		t.Fatalf("fully reported snapshot = %+v, want known activity 2", got)
	}
	registry.recordActiveFetches("legacy", nil)
	if got := registry.RuntimeSnapshot(); got.ActiveFetchesKnown {
		t.Fatalf("legacy heartbeat snapshot = %+v, want unknown activity", got)
	}
	registry.recordActiveFetches(
		"legacy",
		activeFetchReport(yagocrawlcontract.MaximumFetchWorkerConcurrency+1),
	)
	if got := registry.RuntimeSnapshot(); got.ActiveFetchesKnown {
		t.Fatalf("invalid heartbeat snapshot = %+v, want unknown activity", got)
	}
	registry.recordActiveFetches("offline", activeFetchReport(1))
	if got := registry.RuntimeSnapshot(); got.ConnectedCrawlers != 2 || got.ActiveFetchesKnown {
		t.Fatalf("offline heartbeat changed snapshot: %+v", got)
	}
}

func TestCrawlerRuntimeSnapshotExpiresMissedFetchHeartbeats(t *testing.T) {
	registry := newControlRegistry(crawlerControlDefaults{fetchWorkers: 4})
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	now := base
	registry.now = func() time.Time { return now }
	registry.register("worker")
	registry.recordActiveFetches("worker", activeFetchReport(3))
	if got := registry.RuntimeSnapshot(); !got.ActiveFetchesKnown || got.ActiveFetches != 3 {
		t.Fatalf("current fetch report snapshot = %+v", got)
	}
	now = base.Add(crawlerHeartbeatReportLifetime)
	if got := registry.RuntimeSnapshot(); !got.ActiveFetchesKnown || got.ActiveFetches != 3 {
		t.Fatalf("boundary fetch report snapshot = %+v", got)
	}
	now = now.Add(time.Nanosecond)
	if got := registry.RuntimeSnapshot(); got.ActiveFetchesKnown || got.ActiveFetches != 0 {
		t.Fatalf("stale fetch report snapshot = %+v", got)
	}
	registry.recordActiveFetches("worker", activeFetchReport(1))
	if got := registry.RuntimeSnapshot(); !got.ActiveFetchesKnown || got.ActiveFetches != 1 {
		t.Fatalf("recovered fetch report snapshot = %+v", got)
	}
}

func TestCrawlerRuntimeSnapshotUsesLiveFetchLimit(t *testing.T) {
	registry := newControlRegistry(crawlerControlDefaults{fetchWorkers: 4})
	registry.register("worker-1")
	registry.register("worker-2")
	registry.recordActiveFetches("worker-1", activeFetchReport(4))
	registry.recordActiveFetches("worker-2", activeFetchReport(2))
	if signalled := registry.SetFetchWorkers(9); signalled != 2 {
		t.Fatalf("live resize signalled %d crawlers, want 2", signalled)
	}
	if got := registry.RuntimeSnapshot(); got.FetchLimitPerCrawler != 9 ||
		got.AggregateFetchCapacity != 18 || got.ActiveFetches != 6 {
		t.Fatalf("live fetch-limit snapshot = %+v", got)
	}
}

func TestCrawlerRuntimeSnapshotConcurrentAccess(t *testing.T) {
	registry := newControlRegistry(crawlerControlDefaults{fetchWorkers: 8})
	const workers = 16
	for worker := range workers {
		registry.register(workerID(worker))
	}

	var group sync.WaitGroup
	for worker := range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for active := range uint32(100) {
				registry.recordActiveFetches(workerID(worker), activeFetchReport(active%9))
				_ = registry.RuntimeSnapshot()
			}
		}()
	}
	group.Wait()
	if got := registry.RuntimeSnapshot(); got.ConnectedCrawlers != workers ||
		!got.ActiveFetchesKnown || got.FetchLimitPerCrawler != 8 {
		t.Fatalf("concurrent runtime snapshot = %+v", got)
	}
}

func TestExchangeHeartbeatRecordsOnlyConnectedCrawlerActivity(t *testing.T) {
	server := newExchangeServer(
		memQueue(t),
		make(chan crawlresults.IngestDelivery),
		crawlerControlDefaults{fetchWorkers: 4},
	)
	activateTestWorkerSession(t, server, "startup", testWorkerSessionID)
	if _, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId:        "startup",
		WorkerSessionId: testWorkerSessionID,
		ActiveFetches:   activeFetchReport(3),
	}); err != nil {
		t.Fatalf("startup heartbeat: %v", err)
	}
	if got := server.control.RuntimeSnapshot(); got.ConnectedCrawlers != 0 ||
		got.ActiveFetches != 0 || !got.ActiveFetchesKnown {
		t.Fatalf("startup heartbeat created a connected runtime: %+v", got)
	}

	server.control.register("worker")
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	if _, err := server.Heartbeat(
		context.Background(),
		&crawlrpc.WorkerHeartbeat{
			WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
		},
	); err != nil {
		t.Fatalf("legacy heartbeat: %v", err)
	}
	if got := server.control.RuntimeSnapshot(); got.ActiveFetchesKnown {
		t.Fatalf("legacy heartbeat reported known activity: %+v", got)
	}
	if _, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId:        "worker",
		WorkerSessionId: testWorkerSessionID,
		ActiveFetches:   activeFetchReport(0),
	}); err != nil {
		t.Fatalf("current heartbeat: %v", err)
	}
	if got := server.control.RuntimeSnapshot(); got.ActiveFetches != 0 ||
		!got.ActiveFetchesKnown || got.AggregateFetchCapacity != 4 {
		t.Fatalf("current heartbeat snapshot = %+v", got)
	}
	server.releaseWorker("worker")
	if got := server.control.RuntimeSnapshot(); got.ConnectedCrawlers != 0 ||
		got.ActiveFetches != 0 || !got.ActiveFetchesKnown {
		t.Fatalf("released worker snapshot = %+v", got)
	}
}

func workerID(worker int) string {
	return "worker-" + strconv.Itoa(worker)
}
