package crawlorder

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestActiveOrdersBoundsOneHundredSameLeaseReplays(t *testing.T) {
	active := newActiveOrders()
	provenance := []byte("same-order")
	delivery := CrawlOrderDelivery{LeaseID: "same-lease"}
	if claim := active.claim(provenance, delivery); claim != activeOrderStartsRun {
		t.Fatalf("first claim = %d, want start", claim)
	}
	for range 100 {
		if claim := active.claim(provenance, delivery); claim != activeOrderJoinsRun {
			t.Fatalf("active replay claim = %d, want join", claim)
		}
	}

	active.mu.Lock()
	if len(active.deliveries) != 1 {
		t.Fatalf("active deliveries = %d, want 1", len(active.deliveries))
	}
	for _, current := range active.deliveries {
		if current.version != 1 {
			t.Fatalf("same-lease version = %d, want 1", current.version)
		}
	}
	active.mu.Unlock()

	settlements := 0
	active.settle(provenance, delivery, true, func(CrawlOrderDelivery) bool {
		settlements++

		return true
	})
	if settlements != 1 {
		t.Fatalf("settlements = %d, want 1", settlements)
	}
	for range 100 {
		if claim := active.claim(provenance, delivery); claim != activeOrderAlreadyCompleted {
			t.Fatalf("completed replay claim = %d, want completed", claim)
		}
	}

	active.mu.Lock()
	defer active.mu.Unlock()
	if len(active.deliveries) != 0 {
		t.Fatalf("active deliveries after completion = %d, want 0", len(active.deliveries))
	}
	if len(active.completedLeases) != 1 || len(active.completedLeaseOrder) != 1 {
		t.Fatalf(
			"completed retention = %d/%d, want 1/1",
			len(active.completedLeases),
			len(active.completedLeaseOrder),
		)
	}
}

func TestCompletedReplayIsReackedWithoutReseedingAfterAckLoss(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1)
	crawlFrontier := frontier.NewFrontier(4, nil)
	consumer := NewCrawlOrderConsumer(queue, crawlFrontier)
	profile := consumerProfile()
	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte("completed-order"),
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL:           "https://example.org/completed",
			ProfileHandle: profile.Handle,
		}},
	}
	var acknowledgements atomic.Int64
	acknowledged := make(chan struct{}, 101)
	delivery := CrawlOrderDelivery{
		LeaseID: "completed-lease",
		Order:   order,
		Ack: func(context.Context) error {
			acknowledgements.Add(1)
			acknowledged <- struct{}{}

			return errors.New("ack response lost")
		},
		Nak: func(context.Context) error {
			return errors.New("completed order unexpectedly requeued")
		},
	}
	consumer.accept(t.Context(), delivery)
	job, ok := crawlFrontier.Take(t.Context())
	if !ok {
		t.Fatal("frontier closed before completed-order job")
	}
	crawlFrontier.Done(job, successfulPageOutcome())
	select {
	case <-acknowledged:
	case <-time.After(time.Second):
		t.Fatal("initial acknowledgement was not attempted")
	}

	for range 100 {
		consumer.accept(t.Context(), delivery)
	}
	if acknowledgements.Load() != 101 {
		t.Fatalf("acknowledgements after replay = %d, want 101", acknowledgements.Load())
	}
	duplicateCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if duplicate, open := crawlFrontier.Take(duplicateCtx); open {
		t.Fatalf("completed replay seeded duplicate job %q", duplicate.URL)
	}

	consumer.active.mu.Lock()
	defer consumer.active.mu.Unlock()
	if len(consumer.active.deliveries) != 0 {
		t.Fatalf("active deliveries = %d, want 0", len(consumer.active.deliveries))
	}
	if len(consumer.active.completedLeases) != 1 {
		t.Fatalf("completed leases = %d, want 1", len(consumer.active.completedLeases))
	}
}

func TestReplayDuringAckIsSettledWithoutReseeding(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1)
	crawlFrontier := frontier.NewFrontier(4, nil)
	consumer := NewCrawlOrderConsumer(queue, crawlFrontier)
	profile := consumerProfile()
	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte("ack-race-order"),
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL:           "https://example.org/ack-race",
			ProfileHandle: profile.Handle,
		}},
	}
	ackStarted := make(chan struct{})
	releaseAck := make(chan struct{})
	ackFinished := make(chan struct{})
	replayAcknowledged := make(chan struct{})
	initial := CrawlOrderDelivery{
		LeaseID: "ack-race-lease",
		Order:   order,
		Ack: func(context.Context) error {
			defer close(ackFinished)
			close(ackStarted)
			<-releaseAck

			return nil
		},
	}
	replay := CrawlOrderDelivery{
		LeaseID: "ack-race-lease",
		Order:   order,
		Ack: func(context.Context) error {
			close(replayAcknowledged)

			return nil
		},
	}
	consumer.accept(t.Context(), initial)
	job, ok := crawlFrontier.Take(t.Context())
	if !ok {
		t.Fatal("frontier closed before ack-race job")
	}
	crawlFrontier.Done(job, successfulPageOutcome())
	select {
	case <-ackStarted:
	case <-time.After(time.Second):
		t.Fatal("initial acknowledgement did not start")
	}
	consumer.accept(t.Context(), replay)
	select {
	case <-replayAcknowledged:
	case <-time.After(time.Second):
		t.Fatal("replay was not acknowledged during original acknowledgement")
	}
	close(releaseAck)
	select {
	case <-ackFinished:
	case <-time.After(time.Second):
		t.Fatal("initial acknowledgement did not finish")
	}

	duplicateCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if duplicate, open := crawlFrontier.Take(duplicateCtx); open {
		t.Fatalf("ack-race replay seeded duplicate job %q", duplicate.URL)
	}
}

func TestCompletedLeaseRetentionEvictsOldestAtCapacity(t *testing.T) {
	active := newActiveOrders()
	for index := 0; index <= recentCompletedLeaseCapacity; index++ {
		leaseID := fmt.Sprintf("lease-%05d", index)
		provenance := []byte(leaseID)
		delivery := CrawlOrderDelivery{LeaseID: leaseID}
		if claim := active.claim(provenance, delivery); claim != activeOrderStartsRun {
			t.Fatalf("claim %d = %d, want start", index, claim)
		}
		active.settle(provenance, delivery, true, func(CrawlOrderDelivery) bool { return true })
	}

	active.mu.Lock()
	if len(active.completedLeases) != recentCompletedLeaseCapacity ||
		len(active.completedLeaseOrder) != recentCompletedLeaseCapacity {
		t.Fatalf(
			"completed retention = %d/%d, want %d/%d",
			len(active.completedLeases),
			len(active.completedLeaseOrder),
			recentCompletedLeaseCapacity,
			recentCompletedLeaseCapacity,
		)
	}
	_, oldestRetained := active.completedLeases["lease-00000"]
	_, newestRetained := active.completedLeases[fmt.Sprintf(
		"lease-%05d",
		recentCompletedLeaseCapacity,
	)]
	active.mu.Unlock()
	if oldestRetained {
		t.Fatal("oldest completed lease was not evicted")
	}
	if !newestRetained {
		t.Fatal("newest completed lease was not retained")
	}
}

func TestRequeuedLeaseIsNotRetainedAsCompleted(t *testing.T) {
	active := newActiveOrders()
	provenance := []byte("requeued-order")
	delivery := CrawlOrderDelivery{LeaseID: "requeued-lease"}
	if claim := active.claim(provenance, delivery); claim != activeOrderStartsRun {
		t.Fatalf("first claim = %d, want start", claim)
	}
	active.settle(provenance, delivery, false, func(CrawlOrderDelivery) bool { return true })
	if claim := active.claim(provenance, delivery); claim != activeOrderStartsRun {
		t.Fatalf("requeued claim = %d, want start", claim)
	}
}

func TestShutdownSuspendsQueuedRunWithoutSettlingLease(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1)
	crawlFrontier := frontier.NewFrontier(1, nil)
	consumer := NewCrawlOrderConsumer(queue, crawlFrontier)
	profile := consumerProfile()
	settlement := make(chan string, 3)
	consumer.accept(t.Context(), CrawlOrderDelivery{
		LeaseID: "shutdown-lease",
		Order: yagocrawlcontract.CrawlOrder{
			Profile: profile,
			Requests: []yagocrawlcontract.CrawlRequest{{
				URL:           "https://example.org/queued",
				ProfileHandle: profile.Handle,
			}},
		},
		Ack:  func(context.Context) error { settlement <- "ack"; return nil },
		Nak:  func(context.Context) error { settlement <- "nak"; return nil },
		Term: func(context.Context) error { settlement <- "term"; return nil },
	})
	consumer.SuspendActiveRuns()
	consumer.WaitForSettlements()
	select {
	case got := <-settlement:
		t.Fatalf("graceful suspension sent %s", got)
	case <-time.After(20 * time.Millisecond):
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if job, ok := crawlFrontier.Take(ctx); ok {
		t.Fatalf("shutdown returned queued job %q", job.URL)
	}
}

func TestActiveOrdersSettlementFollowsNewLeaseClaim(t *testing.T) {
	active := newActiveOrders()
	provenance := []byte("changing-lease")
	first := CrawlOrderDelivery{LeaseID: "lease-a"}
	latest := CrawlOrderDelivery{LeaseID: "lease-b"}
	if claim := active.claim(provenance, first); claim != activeOrderStartsRun {
		t.Fatalf("first claim = %d, want start", claim)
	}
	var settled []string
	active.settle(provenance, first, true, func(delivery CrawlOrderDelivery) bool {
		settled = append(settled, delivery.LeaseID)
		if delivery.LeaseID == first.LeaseID {
			if claim := active.claim(provenance, latest); claim != activeOrderJoinsRun {
				t.Fatalf("replacement claim = %d, want join", claim)
			}
		}

		return true
	})
	if len(settled) != 2 || settled[0] != first.LeaseID || settled[1] != latest.LeaseID {
		t.Fatalf("settled leases = %v, want [%s %s]", settled, first.LeaseID, latest.LeaseID)
	}
	active.settle(provenance, latest, true, func(CrawlOrderDelivery) bool {
		t.Fatal("settled an order after its active entry was removed")

		return false
	})
}

func TestLeaseIdentityAndDuplicateCompletionStayBounded(t *testing.T) {
	active := newActiveOrders()
	delivery := CrawlOrderDelivery{LeaseID: "lease-only"}
	if claim := active.claim(nil, delivery); claim != activeOrderStartsRun {
		t.Fatalf("lease-only claim = %d, want start", claim)
	}
	alternateProvenance := []byte("alternate-provenance")
	if claim := active.claim(alternateProvenance, delivery); claim != activeOrderStartsRun {
		t.Fatalf("alternate claim = %d, want start", claim)
	}
	active.settle(nil, delivery, true, func(CrawlOrderDelivery) bool { return true })
	active.settle(
		alternateProvenance,
		delivery,
		true,
		func(CrawlOrderDelivery) bool { return true },
	)

	active.mu.Lock()
	defer active.mu.Unlock()
	if len(active.completedLeases) != 1 || len(active.completedLeaseOrder) != 1 {
		t.Fatalf(
			"duplicate completion retention = %d/%d, want 1/1",
			len(active.completedLeases),
			len(active.completedLeaseOrder),
		)
	}
}
