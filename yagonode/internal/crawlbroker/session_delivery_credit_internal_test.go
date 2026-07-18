package crawlbroker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

func TestWorkerSessionDeliveryCreditBoundaries(t *testing.T) {
	credit := newWorkerSessionDeliveryCredit()
	if _, err := credit.expect(nil); err == nil {
		t.Fatal("empty delivery expectation was accepted")
	}
	stopped := newWorkerSessionDeliveryCredit()
	stopped.stop()
	stopped.stop()
	if _, err := stopped.expect([]string{"lease"}); !errors.Is(err, errLeaseLost) {
		t.Fatalf("stopped delivery expectation error = %v", err)
	}
	pending := newWorkerSessionDeliveryCredit()
	confirmation, err := pending.expect([]string{"lease"})
	if err != nil {
		t.Fatalf("expect delivery confirmation: %v", err)
	}
	if _, err := pending.expect([]string{"other"}); err == nil {
		t.Fatal("overlapping delivery expectation was accepted")
	}
	pending.stop()
	if err := confirmation.wait(t.Context()); !errors.Is(err, errLeaseLost) {
		t.Fatalf("stopped delivery wait error = %v", err)
	}
}

func TestWorkerSessionDeliveryCreditFencesMissingAndStaleSessions(t *testing.T) {
	registry := newWorkerSessionRegistry(1)
	if _, err := registry.expectDeliveryConfirmation(
		"missing",
		"session",
		1,
		[]string{"lease"},
	); !errors.Is(err, errLeaseLost) {
		t.Fatalf("missing session expectation error = %v", err)
	}
	registry.confirmDeliveries("missing", "session", []string{"lease"})
	generation, err := registry.activate(
		"worker",
		"session",
		func() {},
		func() error { return nil },
	)
	if err != nil {
		t.Fatalf("activate delivery session: %v", err)
	}
	if _, err := registry.expectDeliveryConfirmation(
		"worker",
		"session",
		generation+1,
		[]string{"lease"},
	); !errors.Is(err, errLeaseLost) {
		t.Fatalf("stale session expectation error = %v", err)
	}
	registry.confirmDeliveries("worker", "stale-session", []string{"lease"})
	registry.deactivate("worker", "session", generation)
	registry.deactivate("worker", "session", generation)
}

type deliveryCreditOrderStream struct {
	grpc.ServerStream
	ctx  context.Context
	sent chan *crawlrpc.CrawlOrderMessage
}

type recoveredDeliveryFixture struct {
	server          *exchangeServer
	stream          *deliveryCreditOrderStream
	waitStarted     <-chan struct{}
	done            <-chan error
	workerID        string
	workerSessionID string
}

func (s *deliveryCreditOrderStream) Context() context.Context {
	return s.ctx
}

func (s *deliveryCreditOrderStream) Send(message *crawlrpc.CrawlOrderMessage) error {
	select {
	case s.sent <- message:
		return nil
	case <-s.ctx.Done():
		return fmt.Errorf("send crawl order: %w", s.ctx.Err())
	}
}

func TestOrdinaryOrderDeliveryWaitsForCurrentLeaseHeartbeat(t *testing.T) {
	waitStarted := captureDeliveryConfirmationWaits(t)
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	for _, name := range []string{"first", "second"} {
		if err := queue.Publish(t.Context(), testOrder(name)); err != nil {
			t.Fatalf("publish %s: %v", name, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream := &deliveryCreditOrderStream{
		ctx:  ctx,
		sent: make(chan *crawlrpc.CrawlOrderMessage, 2),
	}
	done := make(chan error, 1)
	go func() {
		done <- server.StreamOrders(&crawlrpc.WorkerRegistration{
			WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
		}, stream)
	}()
	first := receiveCreditOrder(t, stream.sent)
	waitForDeliveryConfirmation(t, waitStarted)
	if pendingCount(t, queue) != 1 {
		t.Fatal("second order was leased before first delivery confirmation")
	}
	assertCreditOrderUnavailable(t, stream.sent)
	confirmTestWorkerLeases(t, server, "worker", testWorkerSessionID, nil)
	if pendingCount(t, queue) != 1 {
		t.Fatal("empty heartbeat released ordinary delivery credit")
	}
	assertCreditOrderUnavailable(t, stream.sent)
	confirmTestWorkerLeases(
		t,
		server,
		"worker",
		testWorkerSessionID,
		[]string{first.GetLeaseId()},
	)
	second := receiveCreditOrder(t, stream.sent)
	if second.GetLeaseId() == first.GetLeaseId() {
		t.Fatal("second order reused the first lease identity")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("order stream did not stop after delivery wait cancellation")
	}
}

func TestRecoveredOrderDeliveryUsesConfirmedBoundedBatches(t *testing.T) {
	fixture := startRecoveredDelivery(t, maximumRecoveredOrderDeliveryBatch+3)
	firstBatch := receiveRecoveredBatchHeader(t, fixture, maximumRecoveredOrderDeliveryBatch)
	confirmRecoveredBatchFrames(t, fixture, firstBatch, firstBatch, true)
	secondBatch := receiveRecoveredBatchHeader(t, fixture, 3)
	active := append(append([]string(nil), firstBatch...), secondBatch...)
	confirmRecoveredBatchFrames(t, fixture, secondBatch, active, false)
	waitForRecoveredDeliveryCompletion(t, fixture.done)
}

func startRecoveredDelivery(t *testing.T, total int) *recoveredDeliveryFixture {
	t.Helper()
	waitStarted := captureDeliveryConfirmationWaits(t)
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	workerID := "worker"
	workerSessionID := testWorkerSessionID
	for index := range total {
		leaseOneForSession(
			t,
			queue,
			fmt.Sprintf("recovered-%02d", index),
			workerID,
			"previous-session",
		)
	}
	orders, generation, err := server.activateWorkerSession(
		t.Context(),
		workerID,
		workerSessionID,
		func() {},
	)
	if err != nil {
		t.Fatalf("activate recovered worker session: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream := &deliveryCreditOrderStream{
		ctx:  ctx,
		sent: make(chan *crawlrpc.CrawlOrderMessage, len(orders)),
	}
	done := make(chan error, 1)
	go func() {
		done <- server.streamRecoveredOrders(
			stream,
			workerID,
			workerSessionID,
			generation,
			orders,
		)
	}()

	return &recoveredDeliveryFixture{
		server:          server,
		stream:          stream,
		waitStarted:     waitStarted,
		done:            done,
		workerID:        workerID,
		workerSessionID: workerSessionID,
	}
}

func receiveRecoveredBatchHeader(
	t *testing.T,
	fixture *recoveredDeliveryFixture,
	want int,
) []string {
	t.Helper()
	first := receiveCreditOrder(t, fixture.stream.sent)
	waitForDeliveryConfirmation(t, fixture.waitStarted)
	batch := first.GetRecoveredLeaseIds()
	if len(batch) != want {
		t.Fatalf("recovered batch = %d, want %d", len(batch), want)
	}
	assertCreditOrderUnavailable(t, fixture.stream.sent)

	return batch
}

func confirmRecoveredBatchFrames(
	t *testing.T,
	fixture *recoveredDeliveryFixture,
	batch []string,
	active []string,
	rejectRepeatedHeader bool,
) {
	t.Helper()
	confirmTestWorkerLeases(
		t,
		fixture.server,
		fixture.workerID,
		fixture.workerSessionID,
		active,
	)
	for index := 1; index < len(batch); index++ {
		message := receiveCreditOrder(t, fixture.stream.sent)
		if message.GetRecoveredBatchEnd() != (index == len(batch)-1) {
			t.Fatalf(
				"recovered batch frame %d end = %v",
				index,
				message.GetRecoveredBatchEnd(),
			)
		}
		if rejectRepeatedHeader && len(message.GetRecoveredLeaseIds()) != 0 {
			t.Fatalf("recovered batch repeated header at frame %d", index)
		}
	}
}

func waitForRecoveredDeliveryCompletion(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stream recovered batches: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("recovered batch stream did not finish")
	}
}

func TestRecoveredOrderDeliveryStopsAfterSessionLoss(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	generation, err := server.sessions.activate(
		"worker",
		"session",
		func() {},
		func() error { return nil },
	)
	if err != nil {
		t.Fatalf("activate recovered delivery session: %v", err)
	}
	stream := &fakeOrderStream{ctx: t.Context()}
	var replacementErr error
	replaced := false
	stream.onSend = func() {
		if replaced {
			return
		}
		replaced = true
		server.sessions.confirmDeliveries("worker", "session", []string{"lease-a", "lease-b"})
		server.sessions.deactivate("worker", "session", generation)
		_, replacementErr = server.sessions.activate(
			"worker",
			"replacement-session",
			func() {},
			func() error { return nil },
		)
	}
	err = server.streamRecoveredOrders(
		stream,
		"worker",
		"session",
		generation,
		[]leasedCrawlOrder{
			{LeaseID: "lease-a", OrderData: []byte("a")},
			{LeaseID: "lease-b", OrderData: []byte("b")},
		},
	)
	if replacementErr != nil {
		t.Fatalf("activate replacement delivery session: %v", replacementErr)
	}
	if status.Code(err) != codes.FailedPrecondition || len(stream.sent) != 1 {
		t.Fatalf("session-loss recovery result = %v, frames = %d", err, len(stream.sent))
	}
}

func TestOrdinaryOrderDeliveryRejectsOverlappingConfirmation(t *testing.T) {
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if err := queue.Publish(t.Context(), testOrder("overlap")); err != nil {
		t.Fatalf("publish overlapping confirmation order: %v", err)
	}
	generation, err := server.sessions.activate(
		"worker",
		"session",
		func() {},
		func() error { return nil },
	)
	if err != nil {
		t.Fatalf("activate ordinary delivery session: %v", err)
	}
	if _, err := server.sessions.expectDeliveryConfirmation(
		"worker",
		"session",
		generation,
		[]string{"existing"},
	); err != nil {
		t.Fatalf("prepare overlapping confirmation: %v", err)
	}
	err = server.streamNewOrders(
		t.Context(),
		&fakeOrderStream{ctx: t.Context()},
		"worker",
		"session",
		generation,
	)
	if status.Code(err) != codes.Internal {
		t.Fatalf("overlapping confirmation status = %v, want Internal", status.Code(err))
	}
}

func receiveCreditOrder(
	t *testing.T,
	deliveries <-chan *crawlrpc.CrawlOrderMessage,
) *crawlrpc.CrawlOrderMessage {
	t.Helper()
	select {
	case delivery := <-deliveries:
		return delivery
	case <-time.After(time.Second):
		t.Fatal("crawl order was not delivered")
	}

	return nil
}

func assertCreditOrderUnavailable(
	t *testing.T,
	deliveries <-chan *crawlrpc.CrawlOrderMessage,
) {
	t.Helper()
	select {
	case delivery := <-deliveries:
		t.Fatalf("unexpected crawl order delivery %q", delivery.GetLeaseId())
	default:
	}
}

func captureDeliveryConfirmationWaits(t *testing.T) <-chan struct{} {
	t.Helper()
	restore := beforeDeliveryConfirmationWait
	waits := make(chan struct{}, 4)
	beforeDeliveryConfirmationWait = func() { waits <- struct{}{} }
	t.Cleanup(func() { beforeDeliveryConfirmationWait = restore })

	return waits
}

func waitForDeliveryConfirmation(t *testing.T, waits <-chan struct{}) {
	t.Helper()
	select {
	case <-waits:
	case <-time.After(time.Second):
		t.Fatal("crawl order delivery did not enter confirmation wait")
	}
}

func confirmTestWorkerLeases(
	t *testing.T,
	server *exchangeServer,
	workerID string,
	workerSessionID string,
	leaseIDs []string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := server.Heartbeat(ctx, &crawlrpc.WorkerHeartbeat{
		WorkerId: workerID, WorkerSessionId: workerSessionID, ActiveLeaseIds: leaseIDs,
	}); err != nil {
		t.Fatalf("confirm crawl order deliveries: %v", err)
	}
}
