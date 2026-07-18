package crawlorder

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestConsumerBoundsProductionSizedRunWave(t *testing.T) {
	const (
		runs    = 270
		maximum = 4
	)
	crawlFrontier := frontier.NewFrontier(runs, nil)
	admission := NewActiveRunAdmission(maximum)
	queue := boundedqueue.NewBoundedQueue[CrawlOrderDelivery](runs)
	consumer := NewCrawlOrderConsumer(queue, crawlFrontier).
		WithActiveRunAdmission(admission)
	acknowledged := make(chan struct{}, runs)
	for index := range runs {
		delivery := activeRunDelivery(
			fmt.Sprintf("wave-%03d", index),
			fmt.Sprintf("host-%03d.example", index),
			acknowledged,
		)
		if err := queue.Publish(t.Context(), delivery); err != nil {
			t.Fatalf("publish recovery wave %d: %v", index, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		consumer.Run(ctx)
		close(done)
	}()
	active := make([]crawljob.CrawlJob, 0, maximum)
	for range maximum {
		active = append(active, takeActiveRunJob(t, crawlFrontier))
	}
	probeCtx, cancelProbe := context.WithTimeout(t.Context(), 20*time.Millisecond)
	if unexpected, ok := crawlFrontier.Take(probeCtx); ok {
		cancelProbe()
		t.Fatalf("run beyond active maximum was seeded: %s", unexpected.URL)
	}
	cancelProbe()
	for completed := 0; completed < runs; completed++ {
		job := active[0]
		active = active[1:]
		crawlFrontier.Done(job, successfulPageOutcome())
		if completed+1+len(active) < runs {
			active = append(active, takeActiveRunJob(t, crawlFrontier))
		}
		if got := activeRunTotal(admission); got > maximum {
			t.Fatalf("active runs = %d, maximum %d", got, maximum)
		}
	}
	for index := range runs {
		select {
		case <-acknowledged:
		case <-time.After(time.Second):
			t.Fatalf("recovery wave acknowledgment %d was not delivered", index)
		}
	}
	waitForNoActiveRuns(t, admission)
	cancel()
	waitSignal(t, done, "recovery wave consumer did not stop")
}

func TestConsumerStartsNextPreparedRunAfterFrontierCompletion(t *testing.T) {
	crawlFrontier := frontier.NewFrontier(4, nil)
	admission := NewActiveRunAdmission(1)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](2),
		crawlFrontier,
	).WithActiveRunAdmission(admission)
	firstAck := make(chan struct{}, 1)
	secondAck := make(chan struct{})
	consumer.accept(t.Context(), activeRunDelivery("first", "one.example", firstAck))
	secondAccepted := make(chan struct{})
	go func() {
		consumer.accept(t.Context(), activeRunDelivery("second", "two.example", secondAck))
		close(secondAccepted)
	}()
	first := takeActiveRunJob(t, crawlFrontier)
	if first.URL != "https://one.example/" {
		t.Fatalf("first job URL = %q", first.URL)
	}
	select {
	case <-secondAccepted:
		t.Fatal("second run was accepted before frontier completion")
	case <-time.After(20 * time.Millisecond):
	}
	if got := activeRunTotal(admission); got != 1 {
		t.Fatalf("active runs before completion = %d, want 1", got)
	}
	crawlFrontier.Done(first, successfulPageOutcome())
	waitSignal(t, firstAck, "first run was not acknowledged")
	waitSignal(t, secondAccepted, "second prepared run was not admitted")
	second := takeActiveRunJob(t, crawlFrontier)
	if second.URL != "https://two.example/" {
		t.Fatalf("second job URL = %q", second.URL)
	}
	crawlFrontier.Done(second, successfulPageOutcome())
	waitSignal(t, secondAck, "second run was not acknowledged")
	waitForNoActiveRuns(t, admission)
}

func TestConsumerRetainsActiveSlotThroughTerminalSettlement(t *testing.T) {
	crawlFrontier := frontier.NewFrontier(4, nil)
	admission := NewActiveRunAdmission(1)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](2),
		crawlFrontier,
	).WithActiveRunAdmission(admission)
	settlementStarted := make(chan struct{})
	settlementRelease := make(chan struct{})
	first := activeRunDelivery("settling", "settling.example", make(chan struct{}))
	first.Ack = func(context.Context) error {
		close(settlementStarted)
		<-settlementRelease

		return nil
	}
	consumer.accept(t.Context(), first)
	secondAck := make(chan struct{})
	secondAccepted := make(chan struct{})
	go func() {
		consumer.accept(
			t.Context(),
			activeRunDelivery("replacement", "replacement.example", secondAck),
		)
		close(secondAccepted)
	}()
	firstJob := takeActiveRunJob(t, crawlFrontier)
	firstCompleted := make(chan struct{})
	go func() {
		crawlFrontier.Done(firstJob, successfulPageOutcome())
		close(firstCompleted)
	}()
	waitSignal(t, settlementStarted, "terminal settlement did not start")
	select {
	case <-secondAccepted:
		t.Fatal("replacement run was admitted during terminal settlement")
	case <-time.After(20 * time.Millisecond):
	}
	if got := activeRunTotal(admission); got != 1 {
		t.Fatalf("active runs during terminal settlement = %d, want 1", got)
	}
	close(settlementRelease)
	waitSignal(t, firstCompleted, "first frontier completion did not return")
	waitSignal(t, secondAccepted, "replacement run was not admitted after settlement")
	secondJob := takeActiveRunJob(t, crawlFrontier)
	crawlFrontier.Done(secondJob, successfulPageOutcome())
	waitSignal(t, secondAck, "replacement run was not acknowledged")
	waitForNoActiveRuns(t, admission)
}

func TestConsumerDoesNotAdmitDuplicateOrTerminalOrders(t *testing.T) {
	crawlFrontier := frontier.NewFrontier(4, nil)
	admission := NewActiveRunAdmission(1)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](2),
		crawlFrontier,
	).WithActiveRunAdmission(admission)
	ack := make(chan struct{})
	first := activeRunDelivery("active", "active.example", ack)
	first.LeaseID = "active-lease"
	consumer.accept(t.Context(), first)
	duplicateReturned := make(chan struct{})
	go func() {
		consumer.accept(t.Context(), first)
		close(duplicateReturned)
	}()
	waitSignal(t, duplicateReturned, "duplicate active order consumed capacity")
	terminated := make(chan struct{})
	consumer.accept(t.Context(), CrawlOrderDelivery{
		LeaseID: "invalid-lease",
		Order: yagocrawlcontract.CrawlOrder{
			Provenance: []byte("invalid"),
			Profile:    yagocrawlcontract.CrawlProfile{URLMustMatch: "("},
		},
		Term: func(context.Context) error {
			close(terminated)

			return nil
		},
	})
	waitSignal(t, terminated, "terminal order waited for active capacity")
	consumer.active.rememberCompletedLease("completed-lease")
	completed := make(chan struct{}, 1)
	replay := activeRunDelivery("completed", "completed.example", completed)
	replay.LeaseID = "completed-lease"
	consumer.accept(t.Context(), replay)
	waitSignal(t, completed, "completed replay waited for active capacity")
	if got := activeRunTotal(admission); got != 1 {
		t.Fatalf("active runs after nonstarting orders = %d, want 1", got)
	}
	job := takeActiveRunJob(t, crawlFrontier)
	crawlFrontier.Done(job, successfulPageOutcome())
	waitSignal(t, ack, "active run was not acknowledged")
	waitForNoActiveRuns(t, admission)
}

func TestConsumerCancellationWhileAwaitingActiveRunRetainsOrder(t *testing.T) {
	crawlFrontier := frontier.NewFrontier(4, nil)
	admission := NewActiveRunAdmission(1)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](2),
		crawlFrontier,
	).WithActiveRunAdmission(admission)
	firstAck := make(chan struct{})
	consumer.accept(t.Context(), activeRunDelivery("first-cancel", "one.example", firstAck))
	ctx, cancel := context.WithCancel(t.Context())
	settled := make(chan string, 3)
	waiting := activeRunDelivery("waiting-cancel", "two.example", make(chan struct{}))
	waiting.Ack = func(context.Context) error { settled <- "ack"; return nil }
	waiting.Nak = func(context.Context) error { settled <- "nak"; return nil }
	waiting.Term = func(context.Context) error { settled <- "term"; return nil }
	returned := make(chan struct{})
	go func() {
		consumer.accept(ctx, waiting)
		close(returned)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	waitSignal(t, returned, "cancelled active-run wait did not return")
	select {
	case disposition := <-settled:
		t.Fatalf("retained order sent %s", disposition)
	case <-time.After(20 * time.Millisecond):
	}
	if got := activeRunTotal(admission); got != 1 {
		t.Fatalf("active runs after cancelled wait = %d, want 1", got)
	}
	job := takeActiveRunJob(t, crawlFrontier)
	crawlFrontier.Done(job, successfulPageOutcome())
	waitSignal(t, firstAck, "first run was not acknowledged")
	waitForNoActiveRuns(t, admission)
}

func activeRunDelivery(provenance, host string, ack chan<- struct{}) CrawlOrderDelivery {
	profile := consumerProfile()

	return CrawlOrderDelivery{
		LeaseID: provenance + "-lease",
		Order: yagocrawlcontract.CrawlOrder{
			Provenance: []byte(provenance),
			Profile:    profile,
			Requests: []yagocrawlcontract.CrawlRequest{{
				URL:           "https://" + host + "/",
				ProfileHandle: profile.Handle,
			}},
		},
		Ack: func(context.Context) error {
			ack <- struct{}{}

			return nil
		},
	}
}

func takeActiveRunJob(t *testing.T, crawlFrontier *frontier.Frontier) crawljob.CrawlJob {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	job, ok := crawlFrontier.Take(ctx)
	if !ok {
		t.Fatal("frontier did not provide an admitted run")
	}

	return job
}

func waitSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}

func waitForNoActiveRuns(t *testing.T, admission *ActiveRunAdmission) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if activeRunTotal(admission) == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("active runs = %d, want 0", activeRunTotal(admission))
}
