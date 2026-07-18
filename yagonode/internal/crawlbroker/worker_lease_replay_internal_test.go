package crawlbroker

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

func TestStreamOrdersReplaysWorkerLeaseBeforePendingOrder(t *testing.T) {
	set := withClock(t)
	base := time.Unix(4000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	leaseID := leaseOne(t, queue, "leased", "worker-a")
	if err := queue.Publish(context.Background(), testOrder("pending")); err != nil {
		t.Fatalf("publish pending order: %v", err)
	}

	set(base.Add(30 * time.Second))
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	ctx, cancel := context.WithCancel(context.Background())
	sends := 0
	stream := &fakeOrderStream{ctx: ctx, onSend: func() {
		sends++
		if sends == 2 {
			cancel()
		}
	}}
	_ = server.StreamOrders(&crawlrpc.WorkerRegistration{
		WorkerId: "worker-a", WorkerSessionId: testWorkerSessionID,
	}, stream)

	if len(stream.sent) != 2 {
		t.Fatalf("sent %d orders, want replay and pending order", len(stream.sent))
	}
	if stream.sent[0].GetLeaseId() != leaseID {
		t.Fatalf("replayed lease = %q, want %q", stream.sent[0].GetLeaseId(), leaseID)
	}
	for i, want := range []string{"leased", "pending"} {
		order, err := yagocrawlcontract.UnmarshalCrawlOrder(stream.sent[i].GetOrderJson())
		if err != nil {
			t.Fatalf("decode sent order %d: %v", i, err)
		}
		if order.Profile.Name != want {
			t.Fatalf("sent order %d = %q, want %q", i, order.Profile.Name, want)
		}
	}
	record, ok := leaseRecordFor(t, queue, leaseID)
	if !ok {
		t.Fatal("replayed lease disappeared")
	}
	wantDeadline := base.Add(90 * time.Second).UnixNano()
	if record.ExpiresAtUnixNano != wantDeadline {
		t.Fatalf("replayed deadline = %d, want %d", record.ExpiresAtUnixNano, wantDeadline)
	}
}

func TestDisconnectedLeaseCannotMoveToAnotherWorkerBeforeExpiry(t *testing.T) {
	set := withClock(t)
	base := time.Unix(5000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if err := queue.Publish(context.Background(), testOrder("sticky")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	aCtx, cancelA := context.WithCancel(context.Background())
	aStream := &fakeOrderStream{ctx: aCtx, onSend: cancelA}
	_ = server.StreamOrders(&crawlrpc.WorkerRegistration{
		WorkerId: "worker-a", WorkerSessionId: testWorkerSessionID,
	}, aStream)
	leaseID := aStream.sent[0].GetLeaseId()

	parked := signalOnQueueWait(t)
	bCtx, cancelB := context.WithCancel(context.Background())
	bStream := &fakeOrderStream{ctx: bCtx}
	bDone := make(chan error, 1)
	go func() {
		bDone <- server.StreamOrders(
			&crawlrpc.WorkerRegistration{
				WorkerId: "worker-b", WorkerSessionId: testWorkerSessionID,
			},
			bStream,
		)
	}()
	select {
	case <-parked:
	case <-time.After(time.Second):
		t.Fatal("worker B did not wait for pending work")
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want A's order leased", n)
	}

	reconnectCtx, cancelReconnect := context.WithCancel(context.Background())
	reconnect := &fakeOrderStream{ctx: reconnectCtx, onSend: cancelReconnect}
	_ = server.StreamOrders(&crawlrpc.WorkerRegistration{
		WorkerId: "worker-a", WorkerSessionId: testWorkerSessionID,
	}, reconnect)
	if len(reconnect.sent) != 1 || reconnect.sent[0].GetLeaseId() != leaseID {
		t.Fatalf("A reconnect leases = %#v, want same lease %q", reconnect.sent, leaseID)
	}

	cancelB()
	select {
	case <-bDone:
	case <-time.After(time.Second):
		t.Fatal("worker B stream did not stop")
	}
	if len(bStream.sent) != 0 {
		t.Fatalf("worker B received %d orders before expiry", len(bStream.sent))
	}
}

func TestExpiredCheckpointLeaseResumesOnlyOnOwningWorker(t *testing.T) {
	set := withClock(t)
	base := time.Unix(6000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if err := queue.Publish(context.Background(), testOrder("expired")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	aCtx, cancelA := context.WithCancel(context.Background())
	aStream := &fakeOrderStream{ctx: aCtx, onSend: cancelA}
	_ = server.StreamOrders(&crawlrpc.WorkerRegistration{
		WorkerId: "worker-a", WorkerSessionId: testWorkerSessionID,
	}, aStream)
	oldLeaseID := aStream.sent[0].GetLeaseId()

	parked := signalOnQueueWait(t)
	bCtx, cancelB := context.WithCancel(context.Background())
	bStream := &fakeOrderStream{ctx: bCtx}
	bDone := make(chan error, 1)
	go func() {
		bDone <- server.StreamOrders(
			&crawlrpc.WorkerRegistration{
				WorkerId: "worker-b", WorkerSessionId: testWorkerSessionID,
			},
			bStream,
		)
	}()
	select {
	case <-parked:
	case <-time.After(time.Second):
		t.Fatal("worker B did not wait for expired work")
	}
	set(base.Add(2 * time.Minute))
	if err := queue.sweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep expired lease: %v", err)
	}
	if _, found := leaseRecordFor(t, queue, oldLeaseID); !found {
		t.Fatal("owner-affined lease was removed after expiry")
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want owner-affined lease parked", n)
	}
	reconnectContext, cancelReconnect := context.WithCancel(context.Background())
	reconnect := &fakeOrderStream{ctx: reconnectContext, onSend: cancelReconnect}
	if err := server.StreamOrders(
		&crawlrpc.WorkerRegistration{
			WorkerId: "worker-a", WorkerSessionId: "restarted-session",
		},
		reconnect,
	); status.Code(err) != codes.Canceled {
		t.Fatalf("owner reconnect status = %v, want Canceled", status.Code(err))
	}
	if len(reconnect.sent) != 1 || reconnect.sent[0].GetLeaseId() != oldLeaseID {
		t.Fatalf("owner reconnect leases = %#v, want %q", reconnect.sent, oldLeaseID)
	}
	cancelB()
	select {
	case <-bDone:
	case <-time.After(time.Second):
		t.Fatal("worker B stream did not stop")
	}
	if len(bStream.sent) != 0 {
		t.Fatalf("worker B received %d owner-affined orders", len(bStream.sent))
	}
	if err := queue.sweepExpired(context.Background()); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	record, found := leaseRecordFor(t, queue, oldLeaseID)
	if !found || record.WorkerID != "worker-a" ||
		record.WorkerSessionID != "restarted-session" {
		t.Fatalf("resumed lease = %#v/%v", record, found)
	}
}

func TestAckBeforeReplayOmitsSettledLease(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "settled", "worker-a")
	if err := queue.ackLease(context.Background(), leaseID); err != nil {
		t.Fatalf("ack: %v", err)
	}
	leasedOrders, err := queue.leasedOrdersForWorker(context.Background(), "worker-a")
	if err != nil {
		t.Fatalf("read worker leases: %v", err)
	}
	if len(leasedOrders) != 0 {
		t.Fatalf("replays = %d, want 0 after ack", len(leasedOrders))
	}
}

func TestReplaySnapshotBeforeAckDoesNotResurrectLease(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "settled", "worker-a")
	leasedOrders, err := queue.leasedOrdersForWorker(context.Background(), "worker-a")
	if err != nil {
		t.Fatalf("read worker leases: %v", err)
	}
	if len(leasedOrders) != 1 || leasedOrders[0].LeaseID != leaseID {
		t.Fatalf("replay = %#v, want lease %q", leasedOrders, leaseID)
	}
	if err := queue.ackLease(context.Background(), leaseID); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if _, ok := leaseRecordFor(t, queue, leaseID); ok {
		t.Fatal("lease was resurrected after replay snapshot")
	}
}

func TestExpiredWorkerLeaseIsNotReplayed(t *testing.T) {
	set := withClock(t)
	base := time.Unix(7000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	leaseID := leaseOne(t, queue, "expired", "worker-a")

	set(base.Add(time.Minute))
	leasedOrders, err := queue.leasedOrdersForWorker(context.Background(), "worker-a")
	if err != nil {
		t.Fatalf("read worker leases: %v", err)
	}
	if len(leasedOrders) != 0 {
		t.Fatalf("replays = %d, want expired lease omitted", len(leasedOrders))
	}
	record, ok := leaseRecordFor(t, queue, leaseID)
	if !ok || record.ExpiresAtUnixNano != base.Add(time.Minute).UnixNano() {
		t.Fatalf("expired lease changed during replay read: %#v/%v", record, ok)
	}
}

func TestReplayRenewalPreventsOriginalDeadlineSweep(t *testing.T) {
	set := withClock(t)
	base := time.Unix(8000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	leaseID := leaseOne(t, queue, "renewed", "worker-a")

	set(base.Add(59 * time.Second))
	leasedOrders, err := queue.leasedOrdersForWorker(context.Background(), "worker-a")
	if err != nil {
		t.Fatalf("read worker leases: %v", err)
	}
	if len(leasedOrders) != 1 || leasedOrders[0].LeaseID != leaseID {
		t.Fatalf("replays = %#v, want lease %q", leasedOrders, leaseID)
	}
	set(base.Add(61 * time.Second))
	if err := queue.sweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep original deadline: %v", err)
	}
	if _, ok := leaseRecordFor(t, queue, leaseID); !ok {
		t.Fatal("renewed replay lease was swept at its original deadline")
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want renewed lease retained", n)
	}
}

func TestExpirySweepBeforeReplayPreventsStaleSnapshot(t *testing.T) {
	set := withClock(t)
	base := time.Unix(9000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	leaseID := leaseOne(t, queue, "expired", "worker-a")

	set(base.Add(time.Minute))
	if err := queue.sweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep expired lease: %v", err)
	}
	leasedOrders, err := queue.leasedOrdersForWorker(context.Background(), "worker-a")
	if err != nil {
		t.Fatalf("read worker leases: %v", err)
	}
	if len(leasedOrders) != 0 {
		t.Fatalf("replays = %#v, want none after sweep", leasedOrders)
	}
	if _, ok := leaseRecordFor(t, queue, leaseID); ok {
		t.Fatal("expired lease remains after sweep")
	}
	if n := pendingCount(t, queue); n != 1 {
		t.Fatalf("pending = %d, want swept order available once", n)
	}
}

func TestStreamOrdersKeepsLeaseWhenReplaySendFails(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "replay", "worker-a")
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	stream := &fakeOrderStream{ctx: context.Background(), sendErr: errors.New("stream broken")}
	if err := server.StreamOrders(
		&crawlrpc.WorkerRegistration{
			WorkerId: "worker-a", WorkerSessionId: testWorkerSessionID,
		},
		stream,
	); err == nil {
		t.Fatal("expected replay send error")
	}
	if _, ok := leaseRecordFor(t, queue, leaseID); !ok {
		t.Fatal("replay send failure removed the lease")
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want replay lease retained", n)
	}
}

func TestLeasedOrdersForWorkerSurfacesStorageErrors(t *testing.T) {
	scanFailure := scriptedQueue(t)
	scanFailure.engine.scanErrors[leaseBucket] = errors.New("scan failed")
	if _, err := scanFailure.queue.leasedOrdersForWorker(
		context.Background(),
		"worker-a",
	); err == nil {
		t.Fatal("expected replay scan error")
	}

	putFailure := scriptedQueue(t)
	_ = leaseOne(t, putFailure.queue, "replay", "worker-a")
	putFailure.engine.putErrors[leaseBucket] = errors.New("put failed")
	if _, err := putFailure.queue.leasedOrdersForWorker(
		context.Background(),
		"worker-a",
	); err == nil {
		t.Fatal("expected replay renewal error")
	}
}

func TestLeasedOrdersForWorkerDropsAbortedSnapshotState(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOne(t, fixture.queue, "stale", "worker-a")
	fixture.engine.replayNext = true
	fixture.engine.betweenReplay = func() {
		delete(fixture.engine.buckets[leaseBucket], leaseID)
	}
	leasedOrders, err := fixture.queue.leasedOrdersForWorker(
		context.Background(),
		"worker-a",
	)
	if err != nil {
		t.Fatalf("read worker leases: %v", err)
	}
	if len(leasedOrders) != 0 {
		t.Fatalf("replays = %#v, want aborted snapshot discarded", leasedOrders)
	}
}
