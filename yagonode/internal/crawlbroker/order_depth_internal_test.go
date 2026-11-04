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

	server := newExchangeServer(queue, nil)
	generation := activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
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

	if _, _, err := server.leaseNextForSession(
		ctx,
		"worker",
		testWorkerSessionID,
		generation,
	); err != nil {
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

func TestPendingURLSampleIsBoundedAndUsesOldestPendingOrders(t *testing.T) {
	queue := memQueue(t)
	ctx := context.Background()
	for _, name := range []string{"first", "second", "third", "fourth", "fifth"} {
		if err := queue.Publish(ctx, testOrder(name)); err != nil {
			t.Fatalf("publish %s: %v", name, err)
		}
	}

	got, err := queue.PendingURLSample(ctx)
	if err != nil {
		t.Fatalf("sample pending URLs: %v", err)
	}
	want := []string{
		"https://example.org/first",
		"https://example.org/second",
		"https://example.org/third",
		"https://example.org/fourth",
	}
	if len(got) != len(want) {
		t.Fatalf("sample length = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("sample[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestPendingURLSampleReturnsEmptyWhenNoPendingOrders(t *testing.T) {
	queue := memQueue(t)
	got, err := queue.PendingURLSample(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("empty queue sample = %v, %v; want empty success", got, err)
	}
}
