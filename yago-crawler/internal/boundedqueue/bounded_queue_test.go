package boundedqueue_test

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
)

func TestBoundedQueuePublishReceive(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[int](2)
	if err := queue.Publish(context.Background(), 7); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := <-queue.Receive(); got != 7 {
		t.Errorf("received %d, want 7", got)
	}
}

func TestBoundedQueuePublishRespectsContext(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[int](1)
	if err := queue.Publish(context.Background(), 1); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := queue.Publish(ctx, 2); err == nil {
		t.Error("expected error when context cancelled and queue full")
	}
}

func TestBoundedQueueCloseEndsReceive(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[int](1)
	queue.Close()
	if _, ok := <-queue.Receive(); ok {
		t.Error("receive on closed empty queue should report closed")
	}
}
