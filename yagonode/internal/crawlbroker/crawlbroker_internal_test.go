package crawlbroker

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOpenServesAndCloses(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	broker, err := Open(Config{ListenAddr: "127.0.0.1:0"}, v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if broker.Orders == nil || broker.Ingest == nil {
		t.Fatal("broker ports must be wired")
	}
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

	if _, err := Open(Config{ListenAddr: ":0"}, v); err == nil {
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

	if _, err := Open(Config{ListenAddr: "127.0.0.1:0"}, v); err == nil {
		t.Fatal("expected queue registration error")
	}
}

func TestOpenSurfacesLeaseReclaimError(t *testing.T) {
	engine := newScriptedEngine()
	engine.scanErrors[leaseBucket] = errors.New("scan failed")
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	if _, err := Open(Config{ListenAddr: "127.0.0.1:0"}, v); err == nil {
		t.Fatal("expected lease reclaim error")
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
