package yacycrawler_test

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
)

func TestBoundedQueuePublishReceive(t *testing.T) {
	queue := yacycrawler.NewBoundedQueue[int](2)
	if err := queue.Publish(context.Background(), 7); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := <-queue.Receive(); got != 7 {
		t.Errorf("received %d, want 7", got)
	}
}

func TestBoundedQueuePublishRespectsContext(t *testing.T) {
	queue := yacycrawler.NewBoundedQueue[int](1)
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
	queue := yacycrawler.NewBoundedQueue[int](1)
	queue.Close()
	if _, ok := <-queue.Receive(); ok {
		t.Error("receive on closed empty queue should report closed")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := yacycrawler.DefaultConfig()
	if cfg.Workers <= 0 || cfg.JobQueueSize <= 0 || cfg.MaxBodyBytes <= 0 {
		t.Errorf("default config has non-positive bounds: %+v", cfg)
	}
}
