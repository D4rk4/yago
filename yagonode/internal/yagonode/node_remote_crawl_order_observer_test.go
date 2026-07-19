package yagonode

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
)

type remoteCrawlTestQueue struct {
	duplicate bool
	err       error
	calls     int
}

func (q *remoteCrawlTestQueue) PublishOnce(
	context.Context,
	string,
	yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.calls++

	return q.duplicate, q.err
}

type remoteCrawlTestObserver struct {
	mu      sync.Mutex
	calls   int
	orders  []yagocrawlcontract.CrawlOrder
	err     error
	started chan struct{}
	release chan struct{}
	done    chan struct{}
}

type unattachedCrawlProcess struct{}

type stagedCancellationContext struct {
	context.Context
	done  chan struct{}
	calls int
}

func (c *stagedCancellationContext) Done() <-chan struct{} {
	return c.done
}

func (c *stagedCancellationContext) Err() error {
	c.calls++
	if c.calls == 1 {
		return nil
	}

	return context.Canceled
}

func (unattachedCrawlProcess) mountDispatch(*http.ServeMux) {}
func (unattachedCrawlProcess) Run(context.Context)          {}
func (unattachedCrawlProcess) Close()                       {}

func (o *remoteCrawlTestObserver) StageOrder(
	_ context.Context,
	order yagocrawlcontract.CrawlOrder,
) error {
	if o.started != nil {
		select {
		case o.started <- struct{}{}:
		default:
		}
	}
	if o.release != nil {
		<-o.release
	}
	o.mu.Lock()
	o.calls++
	o.orders = append(o.orders, order)
	o.mu.Unlock()
	if o.done != nil {
		select {
		case o.done <- struct{}{}:
		default:
		}
	}

	return o.err
}

func TestRemoteCrawlObserverRunsOnlyAfterNewLocalAcceptance(t *testing.T) {
	queue := &remoteCrawlTestQueue{}
	observer := &remoteCrawlTestObserver{err: errors.New("remote unavailable")}
	observed := remoteCrawlObservedOrderQueue{inner: queue, observer: observer}
	duplicate, err := observed.PublishOnce(t.Context(), "key", yagocrawlcontract.CrawlOrder{})
	if err != nil || duplicate || queue.calls != 1 || observer.calls != 1 {
		t.Fatalf(
			"accepted publish = duplicate %v err %v queue %d observer %d",
			duplicate,
			err,
			queue.calls,
			observer.calls,
		)
	}
	queue.duplicate = true
	duplicate, err = observed.PublishOnce(t.Context(), "key", yagocrawlcontract.CrawlOrder{})
	if err != nil || !duplicate || observer.calls != 1 {
		t.Fatalf(
			"duplicate publish = duplicate %v err %v observer %d",
			duplicate,
			err,
			observer.calls,
		)
	}
	queue.duplicate = false
	queue.err = errors.New("local unavailable")
	if _, err = observed.PublishOnce(
		t.Context(),
		"key",
		yagocrawlcontract.CrawlOrder{},
	); err == nil ||
		observer.calls != 1 {
		t.Fatalf("failed local publish = %v observer %d", err, observer.calls)
	}
}

func TestAsynchronousRemoteCrawlStagerDoesNotBlockLocalPublisher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &remoteCrawlTestObserver{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		done:    make(chan struct{}, 2),
		err:     errors.New("remote unavailable"),
	}
	stager := newRemoteCrawlOrderStager(ctx, sink, 1)
	if err := stager.StageOrder(t.Context(), yagocrawlcontract.CrawlOrder{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("staging worker did not start")
	}
	if err := stager.StageOrder(t.Context(), yagocrawlcontract.CrawlOrder{}); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := stager.StageOrder(t.Context(), yagocrawlcontract.CrawlOrder{}); err == nil {
		t.Fatal("full staging backlog accepted another order")
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("full staging backlog blocked for %v", elapsed)
	}
	close(sink.release)
	select {
	case <-sink.done:
	case <-time.After(time.Second):
		t.Fatal("failed remote staging did not return")
	}
}

func TestKeylessCrawlOrderPublisherPreservesQueueFailure(t *testing.T) {
	t.Parallel()

	queue := &remoteCrawlTestQueue{}
	publisher := keylessCrawlOrderPublisher{queue: queue}
	if err := publisher.Publish(t.Context(), yagocrawlcontract.CrawlOrder{}); err != nil {
		t.Fatal(err)
	}
	queue.err = errors.New("publish failed")
	if err := publisher.Publish(
		t.Context(),
		yagocrawlcontract.CrawlOrder{},
	); !errors.Is(err, queue.err) {
		t.Fatalf("publish failure = %v", err)
	}
}

func TestRemoteCrawlObserverAttachmentUsesSupportedRuntimeOnly(t *testing.T) {
	t.Parallel()

	observer := &remoteCrawlTestObserver{}
	runtime := &crawlRuntime{broker: &crawlbroker.CrawlBroker{}}
	attachRemoteCrawlOrders(runtime, nil)
	if runtime.remoteCrawl != nil {
		t.Fatal("nil observer attached")
	}
	attachRemoteCrawlOrders(unattachedCrawlProcess{}, observer)
	attachRemoteCrawlOrders(runtime, observer)
	if runtime.remoteCrawl != observer {
		t.Fatal("observer was not attached")
	}
	if _, ok := runtime.dispatchQueue().(remoteCrawlObservedOrderQueue); !ok {
		t.Fatal("attached observer did not wrap the dispatch queue")
	}
}

func TestAsynchronousRemoteCrawlStagerHonorsCancellationAndCopiesOrder(t *testing.T) {
	t.Parallel()

	if newRemoteCrawlBrokerOrderStager(t.Context(), nil, 1) != nil {
		t.Fatal("disabled remote crawl produced a stager")
	}
	if newRemoteCrawlOrderStager(t.Context(), nil, 1) != nil {
		t.Fatal("nil sink produced a stager")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	sink := &remoteCrawlTestObserver{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		done:    make(chan struct{}, 1),
	}
	stager := newRemoteCrawlOrderStager(t.Context(), sink, 1)
	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte("provenance"),
		Requests:   []yagocrawlcontract.CrawlRequest{{URL: "https://example.com/"}},
	}
	if err := stager.StageOrder(cancelled, order); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled stage = %v", err)
	}
	done := make(chan struct{})
	close(done)
	betweenChecks := &stagedCancellationContext{Context: t.Context(), done: done}
	blocked := &asynchronousRemoteCrawlStager{
		orders: make(chan yagocrawlcontract.CrawlOrder),
		sink:   sink,
	}
	if err := blocked.StageOrder(betweenChecks, order); !errors.Is(err, context.Canceled) {
		t.Fatalf("concurrent cancellation stage = %v", err)
	}
	if err := stager.StageOrder(t.Context(), order); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("staged order was not received")
	}
	order.Provenance[0] = 'x'
	order.Requests[0].URL = "https://changed.example/"
	close(sink.release)
	select {
	case <-sink.done:
	case <-time.After(time.Second):
		t.Fatal("staged order did not finish")
	}
	sink.mu.Lock()
	staged := sink.orders[0]
	sink.mu.Unlock()
	if string(staged.Provenance) != "provenance" ||
		staged.Requests[0].URL != "https://example.com/" {
		t.Fatalf("staged order was mutated: %+v", staged)
	}
}

func TestAsynchronousRemoteCrawlRunStopsWithParent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	(&asynchronousRemoteCrawlStager{
		orders: make(chan yagocrawlcontract.CrawlOrder),
		sink:   &remoteCrawlTestObserver{},
	}).run(ctx)
}
