package crawlbroker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type orderChannelStream struct {
	grpc.ServerStream
	ctx  context.Context
	sent chan *crawlrpc.CrawlOrderMessage
}

func (s *orderChannelStream) Context() context.Context { return s.ctx }

func (s *orderChannelStream) Send(message *crawlrpc.CrawlOrderMessage) error {
	select {
	case s.sent <- message:
		return nil
	case <-s.ctx.Done():
		return fmt.Errorf("send order on closed stream: %w", s.ctx.Err())
	}
}

func TestDeferredLeaseKeepsOriginalRetryDeadline(t *testing.T) {
	set := withClock(t)
	base := time.Unix(1_500, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "deferred", "worker")
	if err := queue.deferLease(t.Context(), leaseID); err != nil {
		t.Fatalf("first defer: %v", err)
	}
	first, ok := leaseRecordFor(t, queue, leaseID)
	if !ok {
		t.Fatal("deferred lease missing")
	}
	set(base.Add(time.Second))
	if err := queue.deferLease(t.Context(), leaseID); err != nil {
		t.Fatalf("duplicate defer: %v", err)
	}
	second, ok := leaseRecordFor(t, queue, leaseID)
	if !ok || second.ExpiresAtUnixNano != first.ExpiresAtUnixNano {
		t.Fatalf("duplicate defer changed deadline: %#v then %#v", first, second)
	}
}

func TestOwnerlessLeaseWithoutDeferredDispositionIsRejected(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "invalid", "worker")
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		record, found, err := queue.leases.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read lease before owner removal: %w", err)
		}
		if !found {
			return errors.New("lease missing")
		}
		record.WorkerID = ""

		if err := queue.leases.Put(tx, vault.Key(leaseID), record); err != nil {
			return fmt.Errorf("store ownerless lease: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("prepare ownerless lease: %v", err)
	}
	if err := queue.deferLease(t.Context(), leaseID); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("ownerless lease defer = %v", err)
	}
}

func TestAcceptedNakRejectsConflictingAckDuringDelay(t *testing.T) {
	set := withClock(t)
	base := time.Unix(1_750, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "conflict", "worker")
	if err := queue.deferLease(t.Context(), leaseID); err != nil {
		t.Fatalf("defer: %v", err)
	}
	if err := queue.ackLease(t.Context(), leaseID); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("conflicting ack = %v", err)
	}
	record, ok := leaseRecordFor(t, queue, leaseID)
	if !ok || record.WorkerID != "" ||
		record.ExpiresAtUnixNano != base.Add(negativeAcknowledgmentRetryDelay).UnixNano() {
		t.Fatalf("conflicting ack changed deferred lease: %#v/%v", record, ok)
	}
}

func TestExpiredDeferredLeaseRejectsStaleSettlement(t *testing.T) {
	set := withClock(t)
	base := time.Unix(2_000, 0)
	set(base)
	queue := memQueue(t)
	oldLeaseID := leaseOne(t, queue, "stale", "worker")
	if err := queue.deferLease(t.Context(), oldLeaseID); err != nil {
		t.Fatalf("defer: %v", err)
	}
	set(base.Add(negativeAcknowledgmentRetryDelay))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if err := queue.ackLease(
		t.Context(),
		oldLeaseID,
	); !errors.Is(
		err,
		errLeaseDispositionConflict,
	) {
		t.Fatalf("stale ack = %v, want disposition conflict", err)
	}
	if err := queue.deferLease(
		t.Context(),
		oldLeaseID,
	); !errors.Is(
		err,
		errLeaseDispositionConflict,
	) {
		t.Fatalf("stale nak = %v, want disposition conflict", err)
	}
	if n := pendingCount(t, queue); n != 1 {
		t.Fatalf("pending after stale settlements = %d, want 1", n)
	}
	_, newLeaseID, found, err := queue.leasePop(t.Context(), "replacement")
	if err != nil || !found {
		t.Fatalf("replacement lease: found=%v err=%v", found, err)
	}
	if err := queue.ackLease(
		t.Context(),
		oldLeaseID,
	); !errors.Is(
		err,
		errLeaseDispositionConflict,
	) {
		t.Fatalf("stale ack after replacement = %v", err)
	}
	if _, ok := leaseRecordFor(t, queue, newLeaseID); !ok {
		t.Fatal("stale ack deleted replacement lease")
	}
	if err := queue.ackLease(t.Context(), newLeaseID); err != nil {
		t.Fatalf("ack replacement: %v", err)
	}
	if err := queue.ackLease(t.Context(), newLeaseID); err != nil {
		t.Fatalf("duplicate replacement ack: %v", err)
	}
}

func TestDeferredLeaseSurvivesBrokerRestartUntilRetryDeadline(t *testing.T) {
	set := withClock(t)
	base := time.Unix(2_500, 0)
	set(base)
	path := filepath.Join(t.TempDir(), "node.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open first storage: %v", err)
	}
	first, err := Open(Config{ListenAddr: "127.0.0.1:0", LeaseTTL: time.Minute}, storage, nil)
	if err != nil {
		t.Fatalf("open first broker: %v", err)
	}
	leaseID := leaseOne(t, first.Orders, "restart", "worker")
	if err := first.Orders.deferLease(t.Context(), leaseID); err != nil {
		t.Fatalf("defer: %v", err)
	}
	first.Close()
	if err := storage.Close(); err != nil {
		t.Fatalf("close first storage: %v", err)
	}

	set(base.Add(negativeAcknowledgmentRetryDelay - time.Second))
	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open second storage: %v", err)
	}
	second, err := Open(Config{ListenAddr: "127.0.0.1:0", LeaseTTL: time.Minute}, storage, nil)
	if err != nil {
		t.Fatalf("open second broker: %v", err)
	}
	deferred, ok := leaseRecordFor(t, second.Orders, leaseID)
	if !ok || deferred.WorkerID != "" ||
		deferred.ExpiresAtUnixNano != base.Add(negativeAcknowledgmentRetryDelay).UnixNano() {
		t.Fatalf("restart changed deferred lease: %#v/%v", deferred, ok)
	}
	if n := pendingCount(t, second.Orders); n != 0 {
		t.Fatalf("pending before deadline = %d, want 0", n)
	}
	second.Close()
	if err := storage.Close(); err != nil {
		t.Fatalf("close second storage: %v", err)
	}

	set(base.Add(negativeAcknowledgmentRetryDelay))
	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open third storage: %v", err)
	}
	third, err := Open(Config{ListenAddr: "127.0.0.1:0", LeaseTTL: time.Minute}, storage, nil)
	if err != nil {
		t.Fatalf("open third broker: %v", err)
	}
	t.Cleanup(func() {
		third.Close()
		_ = storage.Close()
	})
	if _, ok := leaseRecordFor(t, third.Orders, leaseID); ok {
		t.Fatal("expired deferred lease survived restart")
	}
	if n := pendingCount(t, third.Orders); n != 1 {
		t.Fatalf("pending at deadline = %d, want 1", n)
	}
}

type liveDeferredOrderFixture struct {
	set    func(time.Time)
	base   time.Time
	queue  *DurableOrderQueue
	server *exchangeServer
	stream *orderChannelStream
	done   chan error
	cancel context.CancelFunc
	first  *crawlrpc.CrawlOrderMessage
}

func TestLiveOrderStreamWaitsForDeferredLeaseDeadline(t *testing.T) {
	fixture := newLiveDeferredOrderFixture(t)
	assertDeferredOrderUnavailableBeforeDeadline(t, fixture)
	second := receiveDeferredOrderAtDeadline(t, fixture)
	assertDeferredOrderIsDeliveredOnce(t, fixture)
	finishDeferredOrderStream(t, fixture, second)
}

func newLiveDeferredOrderFixture(t *testing.T) liveDeferredOrderFixture {
	t.Helper()
	set := withClock(t)
	base := time.Unix(3_500, 0)
	set(base)
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if err := queue.Publish(t.Context(), testOrder("live-defer")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	<-queue.notify
	parked := signalOnQueueWait(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream := &orderChannelStream{
		ctx:  ctx,
		sent: make(chan *crawlrpc.CrawlOrderMessage, 2),
	}
	done := make(chan error, 1)
	go func() {
		done <- server.StreamOrders(
			&crawlrpc.WorkerRegistration{
				WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
			},
			stream,
		)
	}()
	first := <-stream.sent
	if _, err := server.AckOrder(t.Context(), &crawlrpc.OrderAck{
		LeaseId: first.GetLeaseId(), Requeue: true,
		WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
	}); err != nil {
		t.Fatalf("nak first delivery: %v", err)
	}
	select {
	case <-parked:
	case <-time.After(time.Second):
		t.Fatal("stream did not wait during nak backoff")
	}
	if n := len(stream.sent); n != 0 {
		t.Fatalf("immediate redeliveries = %d, want 0", n)
	}
	if _, _, found, err := queue.leasePop(t.Context(), "other-worker"); err != nil || found {
		t.Fatalf("lease before retry deadline: found=%v err=%v", found, err)
	}

	return liveDeferredOrderFixture{
		set: set, base: base, queue: queue, server: server,
		stream: stream, done: done, cancel: cancel, first: first,
	}
}

func assertDeferredOrderUnavailableBeforeDeadline(t *testing.T, fixture liveDeferredOrderFixture) {
	t.Helper()
	fixture.set(fixture.base.Add(negativeAcknowledgmentRetryDelay - time.Nanosecond))
	if err := fixture.queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("early sweep: %v", err)
	}
	if n := pendingCount(t, fixture.queue); n != 0 {
		t.Fatalf("pending before retry deadline = %d, want 0", n)
	}
}

func receiveDeferredOrderAtDeadline(
	t *testing.T,
	fixture liveDeferredOrderFixture,
) *crawlrpc.CrawlOrderMessage {
	t.Helper()
	fixture.set(fixture.base.Add(negativeAcknowledgmentRetryDelay))
	if err := fixture.queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("deadline sweep: %v", err)
	}
	var second *crawlrpc.CrawlOrderMessage
	select {
	case second = <-fixture.stream.sent:
	case <-time.After(time.Second):
		t.Fatal("stream did not receive deferred order")
	}
	if second.GetLeaseId() == fixture.first.GetLeaseId() {
		t.Fatal("deferred order retained its settled lease identity")
	}
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(second.GetOrderJson())
	if err != nil || order.Profile.Name != "live-defer" {
		t.Fatalf("redelivery = %#v, err=%v", order, err)
	}

	return second
}

func assertDeferredOrderIsDeliveredOnce(t *testing.T, fixture liveDeferredOrderFixture) {
	t.Helper()
	if err := fixture.queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("repeat sweep: %v", err)
	}
	if n := pendingCount(t, fixture.queue); n != 0 {
		t.Fatalf("pending after redelivery = %d, want 0", n)
	}
	if n := len(fixture.stream.sent); n != 0 {
		t.Fatalf("duplicate redeliveries = %d, want 0", n)
	}
}

func finishDeferredOrderStream(
	t *testing.T,
	fixture liveDeferredOrderFixture,
	second *crawlrpc.CrawlOrderMessage,
) {
	t.Helper()
	if _, err := fixture.server.AckOrder(t.Context(), &crawlrpc.OrderAck{
		LeaseId: second.GetLeaseId(), WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
	}); err != nil {
		t.Fatalf("ack redelivery: %v", err)
	}
	fixture.cancel()
	select {
	case <-fixture.done:
	case <-time.After(time.Second):
		t.Fatal("stream did not stop")
	}
	if _, ok := leaseRecordFor(t, fixture.queue, second.GetLeaseId()); ok {
		t.Fatal("terminal ack retained redelivery lease")
	}
}

func TestLeaseSweepIntervalBoundsScanning(t *testing.T) {
	if got := leaseSweepInterval(DefaultLeaseTTL); got != negativeAcknowledgmentRetryDelay/2 {
		t.Fatalf("default interval = %s", got)
	}
	if got := leaseSweepInterval(time.Second); got != time.Second {
		t.Fatalf("short-lease interval = %s", got)
	}
}
