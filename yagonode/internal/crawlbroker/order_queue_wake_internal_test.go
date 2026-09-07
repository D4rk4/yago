package crawlbroker

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

func TestQueuedBatchWakesEveryWaitingCrawler(t *testing.T) {
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	parked, release := make(chan struct{}, 2), make(chan struct{})
	previous := beforeQueueWait
	beforeQueueWait = func() { parked <- struct{}{}; <-release }
	t.Cleanup(func() { beforeQueueWait = previous })
	completed := make(chan error, 2)
	for _, worker := range []string{"first-worker", "second-worker"} {
		_, generation, err := server.activateWorkerSession(ctx, worker, "session", func() {})
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			_, _, err := server.leaseNextForSession(ctx, worker, "session", generation)
			completed <- err
		}()
	}
	<-parked
	<-parked
	for _, name := range []string{"first", "second"} {
		if err := queue.Publish(ctx, testOrder(name)); err != nil {
			t.Fatal(err)
		}
	}
	close(release)
	for range 2 {
		select {
		case err := <-completed:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(200 * time.Millisecond):
			cancel()
			<-completed
			t.Fatal("queued work did not wake an idle crawler")
		}
	}
}
