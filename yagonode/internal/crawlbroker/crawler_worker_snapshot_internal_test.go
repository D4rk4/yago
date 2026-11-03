package crawlbroker

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestCrawlerWorkerSnapshotsPreserveKnownZeroAndLegacyUnknown(t *testing.T) {
	registry := newControlRegistry(crawlerControlDefaults{fetchWorkers: 8})
	registry.register("worker-current")
	registry.register("worker-waiting")
	registry.register("worker-legacy")
	zero := uint32(0)
	active := uint32(4)
	waiting := uint32(2)
	registry.recordHeartbeatFetchActivity("worker-current", "session-current", &zero, &zero)
	registry.recordHeartbeatFetchActivity("worker-waiting", "session-waiting", &active, &waiting)
	registry.recordHeartbeatFetchActivity("worker-legacy", "session-legacy", &active, nil)
	snapshots := registry.WorkerSnapshots()
	if len(snapshots) != 3 {
		t.Fatalf("worker snapshots = %+v, want three connected workers", snapshots)
	}
	if snapshots[0].WorkerID != "worker-current" || !snapshots[0].ActiveFetchesKnown ||
		!snapshots[0].FetchAdmissionWaitingKnown || snapshots[0].FetchAdmissionWaiting != 0 ||
		snapshots[0].ProcessIdentifier != "session-current" {
		t.Fatalf("current worker snapshot = %+v, want known zero counts", snapshots[0])
	}
	if snapshots[1].WorkerID != "worker-legacy" || !snapshots[1].ActiveFetchesKnown ||
		snapshots[1].ActiveFetches != active || snapshots[1].FetchAdmissionWaitingKnown {
		t.Fatalf(
			"legacy worker snapshot = %+v, want active known and waiting unknown",
			snapshots[1],
		)
	}
	if snapshots[2].WorkerID != "worker-waiting" || snapshots[2].ActiveFetches != active ||
		snapshots[2].FetchAdmissionWaiting != waiting || !snapshots[2].FetchAdmissionWaitingKnown {
		t.Fatalf("waiting worker snapshot = %+v, want active 4 and waiting 2", snapshots[2])
	}
}

func TestCrawlerWorkerSnapshotsExpireStaleActivity(t *testing.T) {
	registry := newControlRegistry()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.register("worker-stale")
	active := uint32(3)
	waiting := uint32(1)
	registry.recordHeartbeatFetchActivity("worker-stale", "session-stale", &active, &waiting)
	current := registry.WorkerSnapshots()
	if len(current) != 1 || !current[0].ActiveFetchesKnown ||
		!current[0].FetchAdmissionWaitingKnown {
		t.Fatalf("fresh worker snapshot = %+v, want both reports known", current)
	}
	now = now.Add(crawlerHeartbeatReportLifetime + time.Nanosecond)
	stale := registry.WorkerSnapshots()
	if len(stale) != 1 || stale[0].ActiveFetchesKnown || stale[0].FetchAdmissionWaitingKnown ||
		stale[0].ProcessIdentifier != "session-stale" || !stale[0].ObservedAt.Equal(current[0].ObservedAt) {
		t.Fatalf(
			"stale worker snapshot = %+v, want unknown measurements and a current process label",
			stale,
		)
	}
}

func TestCrawlerWorkerSnapshotStartsNewProcessWithoutOldActivity(t *testing.T) {
	registry := newControlRegistry()
	registry.register("worker", "process-old")
	active := uint32(3)
	registry.recordHeartbeatFetchActivity("worker", "process-old", &active, nil)
	registry.register("worker", "process-new")
	snapshots := registry.WorkerSnapshots()
	if len(snapshots) != 1 || snapshots[0].ProcessIdentifier != "process-new" ||
		snapshots[0].ActiveFetchesKnown || !snapshots[0].ObservedAt.IsZero() {
		t.Fatalf(
			"replacement process snapshot = %+v, want new identity and no heartbeat report",
			snapshots,
		)
	}
}

func TestHeartbeatRejectsImpossibleFetchAdmissionCounts(t *testing.T) {
	active := uint32(2)
	waiting := uint32(3)
	if validHeartbeatFetchActivity(&crawlrpc.WorkerHeartbeat{
		ActiveFetches: &active, FetchAdmissionWaiting: &waiting,
	}) {
		t.Fatal("heartbeat with more waiters than active jobs was admitted")
	}
	if validHeartbeatFetchActivity(&crawlrpc.WorkerHeartbeat{FetchAdmissionWaiting: &waiting}) {
		t.Fatal("heartbeat with waiters and no active count was admitted")
	}
	tooMany := uint32(yagocrawlcontract.MaximumFetchWorkerConcurrency + 1)
	if validHeartbeatFetchActivity(&crawlrpc.WorkerHeartbeat{ActiveFetches: &tooMany}) {
		t.Fatal("heartbeat above maximum active concurrency was admitted")
	}
	if !validHeartbeatFetchActivity(&crawlrpc.WorkerHeartbeat{ActiveFetches: &active}) {
		t.Fatal("valid legacy active count was rejected")
	}
	zero := uint32(0)
	if !validHeartbeatFetchActivity(&crawlrpc.WorkerHeartbeat{
		ActiveFetches: &zero, FetchAdmissionWaiting: &zero,
	}) {
		t.Fatal("valid explicit zero activity was rejected")
	}
}
