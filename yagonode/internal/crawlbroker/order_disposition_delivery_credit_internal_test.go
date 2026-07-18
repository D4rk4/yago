package crawlbroker

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

func TestSuccessfulOrderDispositionReleasesExactDeliveryCredit(t *testing.T) {
	for _, test := range []struct {
		name    string
		requeue bool
	}{
		{name: "acknowledgment"},
		{name: "requeue", requeue: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, leaseID, confirmation := settlementCreditFixture(t, test.name)
			if _, err := server.AckOrder(t.Context(), &crawlrpc.OrderAck{
				LeaseId: leaseID, Requeue: test.requeue,
				WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
			}); err != nil {
				t.Fatalf("settle order: %v", err)
			}
			assertDeliveryCreditReleased(t, confirmation)
		})
	}
}

func TestIdempotentOrderDispositionReleasesExactDeliveryCredit(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "idempotent", "worker", testWorkerSessionID)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	request := &crawlrpc.OrderAck{
		LeaseId: leaseID, WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
	}
	if _, err := server.AckOrder(t.Context(), request); err != nil {
		t.Fatalf("initial order settlement: %v", err)
	}
	confirmation := expectSettlementCredit(t, server, leaseID)
	if _, err := server.AckOrder(t.Context(), request); err != nil {
		t.Fatalf("idempotent order settlement: %v", err)
	}
	assertDeliveryCreditReleased(t, confirmation)
}

func TestRichTerminalDispositionReleasesExactDeliveryCredit(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "terminal-credit", "worker")
	request := terminalOrderAcknowledgment(t, queue, leaseID, "worker", false)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, request.GetWorkerId(), request.GetWorkerSessionId())
	confirmation := expectWorkerSettlementCredit(
		t,
		server,
		request.GetWorkerId(),
		request.GetWorkerSessionId(),
		leaseID,
	)
	result, err := server.AckOrder(t.Context(), request)
	if err != nil {
		t.Fatalf("settle terminal order: %v", err)
	}
	if len(result.GetConfirmationToken()) == 0 {
		t.Fatal("terminal settlement did not return a confirmation token")
	}
	assertDeliveryCreditReleased(t, confirmation)
}

func TestOrderDispositionDoesNotReleaseUnrelatedDeliveryCredit(t *testing.T) {
	queue := memQueue(t)
	firstLeaseID := leaseOneForSession(t, queue, "expected", "worker", testWorkerSessionID)
	secondLeaseID := leaseOneForSession(t, queue, "settled", "worker", testWorkerSessionID)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	confirmation := expectSettlementCredit(t, server, firstLeaseID)
	if _, err := server.AckOrder(t.Context(), &crawlrpc.OrderAck{
		LeaseId: secondLeaseID, WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
	}); err != nil {
		t.Fatalf("settle unrelated order: %v", err)
	}
	assertDeliveryCreditHeld(t, confirmation)
}

func TestFailedOrderDispositionDoesNotReleaseDeliveryCredit(t *testing.T) {
	server, leaseID, confirmation := settlementCreditFixture(t, "unauthorized")
	server.sessions.confirmDisposition("worker", "other-session", leaseID)
	assertDeliveryCreditHeld(t, confirmation)
	_, err := server.AckOrder(t.Context(), &crawlrpc.OrderAck{
		LeaseId: leaseID, WorkerId: "worker", WorkerSessionId: "other-session",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("unauthorized settlement status = %v", status.Code(err))
	}
	assertDeliveryCreditHeld(t, confirmation)
}

func settlementCreditFixture(
	t *testing.T,
	name string,
) (*exchangeServer, string, *crawlOrderDeliveryConfirmation) {
	t.Helper()
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, name, "worker", testWorkerSessionID)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)

	return server, leaseID, expectSettlementCredit(t, server, leaseID)
}

func expectSettlementCredit(
	t *testing.T,
	server *exchangeServer,
	leaseID string,
) *crawlOrderDeliveryConfirmation {
	t.Helper()

	return expectWorkerSettlementCredit(
		t,
		server,
		"worker",
		testWorkerSessionID,
		leaseID,
	)
}

func expectWorkerSettlementCredit(
	t *testing.T,
	server *exchangeServer,
	workerID string,
	workerSessionID string,
	leaseID string,
) *crawlOrderDeliveryConfirmation {
	t.Helper()
	current := server.sessions.registration(workerID)
	confirmation, err := server.sessions.expectDeliveryConfirmation(
		workerID,
		workerSessionID,
		current.generation,
		[]string{leaseID},
	)
	if err != nil {
		t.Fatalf("expect settlement delivery credit: %v", err)
	}

	return confirmation
}

func assertDeliveryCreditReleased(t *testing.T, confirmation *crawlOrderDeliveryConfirmation) {
	t.Helper()
	select {
	case <-confirmation.confirmed:
	default:
		t.Fatal("order disposition did not release delivery credit")
	}
}

func assertDeliveryCreditHeld(t *testing.T, confirmation *crawlOrderDeliveryConfirmation) {
	t.Helper()
	select {
	case <-confirmation.confirmed:
		t.Fatal("order disposition released unrelated delivery credit")
	default:
	}
}
