package crawlorder

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type statusFailureCheckpoint struct {
	frontier.Checkpoint
	err error
}

func (checkpoint statusFailureCheckpoint) Inspect(
	context.Context,
	[]byte,
	[]byte,
) (frontiercheckpoint.RunState, error) {
	return frontiercheckpoint.RunState{Status: frontiercheckpoint.RunMissing}, checkpoint.err
}

type deletionFailureCheckpoint struct {
	frontier.Checkpoint
	err error
}

func (checkpoint deletionFailureCheckpoint) Delete(context.Context, []byte) error {
	return checkpoint.err
}

type deletionObservationCheckpoint struct {
	frontier.Checkpoint
	deletions *atomic.Int64
}

func (checkpoint deletionObservationCheckpoint) Delete(context.Context, []byte) error {
	checkpoint.deletions.Add(1)

	return nil
}

func TestCheckpointRecoveryFailureRequeuesWithoutDispatch(t *testing.T) {
	sentinel := errors.New("checkpoint status failed")
	crawlFrontier := frontier.NewFrontier(
		1,
		nil,
		frontier.WithCheckpoint(statusFailureCheckpoint{err: sentinel}),
	)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		crawlFrontier,
	)
	naked := make(chan struct{})
	consumer.accept(t.Context(), CrawlOrderDelivery{
		LeaseID: "checkpoint-failure-lease",
		Order:   identityTestOrder(),
		Nak: func(context.Context) error {
			close(naked)

			return nil
		},
	})
	waitCallback(t, naked)
	if !errors.Is(crawlFrontier.CheckpointFailure(), sentinel) {
		t.Fatalf("checkpoint failure = %v, want %v", crawlFrontier.CheckpointFailure(), sentinel)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if job, ok := crawlFrontier.Take(ctx); ok {
		t.Fatalf("checkpoint failure dispatched %q", job.URL)
	}
}

func TestRecoveredSettlementFailureRetainsCheckpoint(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "ack", true: "nak"}[failed], func(t *testing.T) {
			var deletions atomic.Int64
			crawlFrontier := frontier.NewFrontier(
				1,
				nil,
				frontier.WithCheckpoint(deletionObservationCheckpoint{
					deletions: &deletions,
				}),
			)
			consumer := NewCrawlOrderConsumer(
				boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
				crawlFrontier,
			)
			order := identityTestOrder()
			delivery := CrawlOrderDelivery{
				LeaseID: "failed-settlement",
				Order:   order,
				Ack: func(context.Context) error {
					return errors.New("ack failed")
				},
				Nak: func(context.Context) error {
					return errors.New("nak failed")
				},
			}
			if claim := consumer.active.claim(
				order.Provenance,
				delivery,
			); claim != activeOrderStartsRun {
				t.Fatalf("claim = %d, want start", claim)
			}
			consumer.settleRecoveredOrder(t.Context(), order, delivery, frontier.RunRecovery{
				Completed: true,
				Failed:    failed,
			})
			if deletions.Load() != 0 {
				t.Fatalf("checkpoint deletions = %d, want 0", deletions.Load())
			}
		})
	}
}

func TestCheckpointDeletionFailureStopsCrawler(t *testing.T) {
	sentinel := errors.New("checkpoint deletion failed")
	crawlFrontier := frontier.NewFrontier(
		1,
		nil,
		frontier.WithCheckpoint(deletionFailureCheckpoint{err: sentinel}),
	)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		crawlFrontier,
	)
	consumer.forgetCheckpoint(t.Context(), identityTestOrder())
	if !errors.Is(crawlFrontier.CheckpointFailure(), sentinel) {
		t.Fatalf("checkpoint failure = %v, want %v", crawlFrontier.CheckpointFailure(), sentinel)
	}
}

func TestCheckpointDeletionWaitsForLatestLeaseSettlement(t *testing.T) {
	var deletions atomic.Int64
	crawlFrontier := frontier.NewFrontier(
		1,
		nil,
		frontier.WithCheckpoint(deletionObservationCheckpoint{deletions: &deletions}),
	)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		crawlFrontier,
	)
	order := identityTestOrder()
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	latestStarted := make(chan struct{})
	releaseLatest := make(chan struct{})
	settled := make(chan struct{})
	first := CrawlOrderDelivery{
		LeaseID: "first-lease",
		Order:   order,
		Ack: func(context.Context) error {
			close(firstStarted)
			<-releaseFirst

			return nil
		},
	}
	latest := CrawlOrderDelivery{
		LeaseID: "latest-lease",
		Order:   order,
		Ack: func(context.Context) error {
			close(latestStarted)
			<-releaseLatest

			return nil
		},
	}
	if claim := consumer.active.claim(order.Provenance, first); claim != activeOrderStartsRun {
		t.Fatalf("first claim = %d, want start", claim)
	}
	go func() {
		consumer.settleRecoveredOrder(t.Context(), order, first, frontier.RunRecovery{
			Completed: true,
		})
		close(settled)
	}()
	waitCallback(t, firstStarted)
	if claim := consumer.active.claim(order.Provenance, latest); claim != activeOrderJoinsRun {
		t.Fatalf("latest claim = %d, want join", claim)
	}
	close(releaseFirst)
	waitCallback(t, latestStarted)
	if got := deletions.Load(); got != 0 {
		t.Fatalf("checkpoint deletions before latest settlement = %d, want 0", got)
	}
	close(releaseLatest)
	waitCallback(t, settled)
	if got := deletions.Load(); got != 1 {
		t.Fatalf("checkpoint deletions after latest settlement = %d, want 1", got)
	}
}

func TestCancelActiveRunsCancelsQueuedOrder(t *testing.T) {
	crawlFrontier := frontier.NewFrontier(1, nil)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		crawlFrontier,
	)
	profile := consumerProfile()
	terminated := make(chan struct{})
	consumer.accept(t.Context(), CrawlOrderDelivery{
		LeaseID: "cancelled-lease",
		Order: yagocrawlcontract.CrawlOrder{
			Provenance: []byte("cancelled-order"),
			Profile:    profile,
			Requests: []yagocrawlcontract.CrawlRequest{{
				URL:           "https://example.org/cancelled",
				ProfileHandle: profile.Handle,
			}},
		},
		Term: func(context.Context) error {
			close(terminated)

			return nil
		},
	})
	consumer.CancelActiveRuns()
	waitCallback(t, terminated)
	consumer.WaitForSettlements()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if job, ok := crawlFrontier.Take(ctx); ok {
		t.Fatalf("cancelled order dispatched %q", job.URL)
	}
}
