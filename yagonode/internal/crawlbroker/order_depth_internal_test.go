package crawlbroker

import (
	"context"
	"testing"
)

func TestQueueDepthOutstanding(t *testing.T) {
	depth := QueueDepth{Pending: 4, Leased: 3}
	if got := depth.Outstanding(); got != 7 {
		t.Fatalf("outstanding = %d, want 7", got)
	}
}

func TestDurableOrderQueueDepthCountsPendingAndLeased(t *testing.T) {
	queue := memQueue(t)
	ctx := context.Background()

	depth, err := queue.Depth(ctx)
	if err != nil {
		t.Fatalf("empty depth: %v", err)
	}
	if depth.Pending != 0 || depth.Leased != 0 {
		t.Fatalf("empty depth = %+v, want zero", depth)
	}

	for _, name := range []string{"a", "b", "c"} {
		if err := queue.Publish(ctx, testOrder(name)); err != nil {
			t.Fatalf("publish %s: %v", name, err)
		}
	}

	depth, err = queue.Depth(ctx)
	if err != nil {
		t.Fatalf("pending depth: %v", err)
	}
	if depth.Pending != 3 || depth.Leased != 0 || depth.Outstanding() != 3 {
		t.Fatalf("pending depth = %+v, want 3 pending", depth)
	}

	if _, _, err := queue.leaseNext(ctx, "worker"); err != nil {
		t.Fatalf("lease: %v", err)
	}

	depth, err = queue.Depth(ctx)
	if err != nil {
		t.Fatalf("leased depth: %v", err)
	}
	if depth.Pending != 2 || depth.Leased != 1 || depth.Outstanding() != 3 {
		t.Fatalf("leased depth = %+v, want 2 pending / 1 leased", depth)
	}
}
