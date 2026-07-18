package crawlbroker

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOpenServesAndCloses(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	broker, err := Open(Config{ListenAddr: "127.0.0.1:0"}, v, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if broker.Orders == nil || broker.Ingest == nil {
		t.Fatal("broker ports must be wired")
	}
	broker.Close()
}

func TestNilCrawlBrokerCloseIsSafe(t *testing.T) {
	var broker *CrawlBroker
	broker.Close()
}

func TestOpenReturnsListenError(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	restore := listenCrawlRPC
	t.Cleanup(func() { listenCrawlRPC = restore })
	listenCrawlRPC = func(string) (net.Listener, error) { return nil, errors.New("listen failed") }

	if _, err := Open(Config{ListenAddr: ":0"}, v, nil); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestOpenReturnsQueueError(t *testing.T) {
	engine := newScriptedEngine()
	engine.provisionErrors[orderBucket] = errors.New("provision failed")
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	if _, err := Open(Config{ListenAddr: "127.0.0.1:0"}, v, nil); err == nil {
		t.Fatal("expected queue registration error")
	}
}

func TestOpenReturnsControlRegistryError(t *testing.T) {
	engine := newScriptedEngine()
	engine.provisionErrors[controlDirectiveBucket] = errors.New("provision failed")
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	if _, err := Open(Config{ListenAddr: "127.0.0.1:0"}, v, nil); err == nil {
		t.Fatal("expected control registry error")
	}
}

func TestOpenReturnsControlCompletionReplayError(t *testing.T) {
	engine := newScriptedEngine()
	engine.scanErrors[leaseControlTargetBucket] = errors.New("scan failed")
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	if _, err := Open(Config{ListenAddr: "127.0.0.1:0"}, v, nil); err == nil {
		t.Fatal("expected control completion replay error")
	}
}

func TestOpenSurfacesExpiredLeaseReclaimError(t *testing.T) {
	engine := newScriptedEngine()
	engine.scanErrors[leaseBucket] = errors.New("scan failed")
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	if _, err := Open(Config{ListenAddr: "127.0.0.1:0"}, v, nil); err == nil {
		t.Fatal("expected lease reclaim error")
	}
}

func TestOpenRequeuesOnlyExpiredLeasesAndPreservesWorkerReplay(t *testing.T) {
	set := withClock(t)
	base := time.Unix(10_000, 0)
	set(base)
	path := filepath.Join(t.TempDir(), "node.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	first, err := Open(Config{
		ListenAddr: "127.0.0.1:0",
		LeaseTTL:   time.Minute,
	}, storage, nil)
	if err != nil {
		t.Fatalf("open first broker: %v", err)
	}
	expiredLeaseID := leaseOne(t, first.Orders, "expired", "expired-worker")
	set(base.Add(45 * time.Second))
	liveLeaseID := leaseOne(t, first.Orders, "live", "live-worker")
	first.Close()
	if err := storage.Close(); err != nil {
		t.Fatalf("close first storage: %v", err)
	}

	set(base.Add(75 * time.Second))
	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	second, err := Open(Config{
		ListenAddr: "127.0.0.1:0",
		LeaseTTL:   time.Minute,
	}, storage, nil)
	if err != nil {
		t.Fatalf("open second broker: %v", err)
	}
	t.Cleanup(func() {
		second.Close()
		_ = storage.Close()
	})

	if _, ok := leaseRecordFor(t, second.Orders, expiredLeaseID); ok {
		t.Fatal("expired lease remained after broker restart")
	}
	if n := pendingCount(t, second.Orders); n != 1 {
		t.Fatalf("pending = %d, want only the expired order", n)
	}
	replayed, err := second.Orders.leasedOrdersForWorker(
		context.Background(),
		"live-worker",
	)
	if err != nil {
		t.Fatalf("replay live worker: %v", err)
	}
	if len(replayed) != 1 || replayed[0].LeaseID != liveLeaseID {
		t.Fatalf("replayed leases = %#v, want live lease %q", replayed, liveLeaseID)
	}
	if n := pendingCount(t, second.Orders); n != 1 {
		t.Fatalf("pending after replay = %d, want expired order only", n)
	}
}

func TestSweepLeasesReclaimsExpiredOnTick(t *testing.T) {
	set := withClock(t)
	base := time.Unix(3000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	_ = leaseOne(t, queue, "stale", "w1")
	set(base.Add(2 * time.Minute))

	ctx, cancel := context.WithCancel(context.Background())
	tick := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		sweepLeases(ctx, queue, tick)
		close(done)
	}()

	tick <- time.Unix(0, 0)
	deadline := time.After(2 * time.Second)
	for pendingCount(t, queue) == 0 {
		select {
		case <-deadline:
			t.Fatal("expired lease was not reclaimed on tick")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sweepLeases did not stop on cancel")
	}
}

func TestSweepLeasesLogsSweepError(t *testing.T) {
	fixture := scriptedQueue(t)
	fixture.engine.scanErrors[leaseBucket] = errors.New("scan failed")

	ctx, cancel := context.WithCancel(context.Background())
	tick := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		sweepLeases(ctx, fixture.queue, tick)
		close(done)
	}()

	tick <- time.Unix(0, 0)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sweepLeases did not stop after a failing sweep")
	}
}
