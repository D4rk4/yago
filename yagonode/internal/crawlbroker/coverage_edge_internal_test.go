package crawlbroker

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func TestOpenWiresProgressSink(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	broker, err := Open(Config{ListenAddr: "127.0.0.1:0"}, v, &recordingProgressSink{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(broker.Close)

	if broker.Orders == nil || broker.Ingest == nil {
		t.Fatal("broker ports must be wired")
	}
}

func TestDurableOrderQueueDepthSurfacesPendingCountError(t *testing.T) {
	fixture := scriptedQueue(t)
	// A corrupt (non-8-byte) length counter makes the pending Len read fail.
	fixture.engine.buckets["__lengths__"][string(orderBucket)] = []byte{1, 2, 3}

	if _, err := fixture.queue.Depth(context.Background()); err == nil {
		t.Fatal("expected a pending-count error")
	}
}

func TestDurableOrderQueueDepthSurfacesLeasedCountError(t *testing.T) {
	fixture := scriptedQueue(t)
	// Leave the pending counter valid so Len succeeds, then corrupt the lease one.
	fixture.engine.buckets["__lengths__"][string(leaseBucket)] = []byte{1, 2, 3}

	if _, err := fixture.queue.Depth(context.Background()); err == nil {
		t.Fatal("expected a leased-count error")
	}
}
