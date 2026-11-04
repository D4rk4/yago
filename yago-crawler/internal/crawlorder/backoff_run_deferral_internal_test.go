package crawlorder

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type consumerBackoffPace struct{ deadline time.Time }

func (pace consumerBackoffPace) DueAt(_ crawljob.CrawlJob, _ time.Time) time.Time {
	return pace.deadline
}

func (consumerBackoffPace) Visited(crawljob.CrawlJob, time.Time) {}

func (pace consumerBackoffPace) BackoffUntil(string) time.Time {
	return pace.deadline
}

type backoffDeferralObservation struct{ observed chan struct{} }

func (observer *backoffDeferralObservation) ObserveBackoffRunDeferral() {
	observer.observed <- struct{}{}
}

func TestRunDefersBackedOffOrderAndReportsRetryDeadline(t *testing.T) {
	deadline := time.Now().Add(2 * time.Minute)
	frontier := frontier.NewFrontier(1, consumerBackoffPace{deadline: deadline})
	queue := boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1)
	observer := &backoffDeferralObservation{observed: make(chan struct{}, 1)}
	consumer := NewCrawlOrderConsumer(queue, frontier).WithBackoffRunDeferralObserver(observer)
	profile := consumerProfile()
	settled := make(chan time.Duration, 1)
	consumer.accept(context.Background(), CrawlOrderDelivery{
		LeaseID: "backed-off-order",
		Order: yagocrawlcontract.CrawlOrder{
			Provenance: []byte("backed-off-order"),
			Profile:    profile,
			Requests: []yagocrawlcontract.CrawlRequest{{
				URL: "https://example.org/page", ProfileHandle: profile.Handle,
			}},
		},
		Ack: func(context.Context) error {
			t.Error("backed-off order was acknowledged")
			return nil
		},
		Nak: func(context.Context) error {
			t.Error("backed-off order used an immediate requeue")
			return nil
		},
		NakAfter: func(_ context.Context, retryAfter time.Duration) error {
			settled <- retryAfter
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		consumer.Run(ctx)
		close(done)
	}()
	select {
	case <-observer.observed:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("backoff deferral was not observed")
	}
	select {
	case retryAfter := <-settled:
		if retryAfter <= 0 || retryAfter > 2*time.Minute {
			t.Fatalf("retry delay = %v, want a positive bounded delay", retryAfter)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("backed-off order was not requeued with its deadline")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop after cancellation")
	}
}
