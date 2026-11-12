package crawlbroker

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestAckOrderUsesRequestedRetryDelay(t *testing.T) {
	set := withClock(t)
	base := time.Unix(1_700, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "requested-retry", "worker", testWorkerSessionID)
	server := newExchangeServer(queue, nil)
	retryAfter := 11 * time.Minute

	if _, err := server.AckOrder(t.Context(), &crawlrpc.OrderAck{
		LeaseId: leaseID, Requeue: true,
		WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
		RetryAfterNanoseconds: int64(retryAfter),
	}); err != nil {
		t.Fatalf("defer with requested retry delay: %v", err)
	}
	record, found := leaseRecordFor(t, queue, leaseID)
	if !found || record.WorkerID != "" ||
		record.ExpiresAtUnixNano != base.Add(retryAfter).UnixNano() {
		t.Fatalf("deferred lease = %#v/%v", record, found)
	}
}

func TestAckOrderRejectsRetryDelayForTerminalDisposition(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "terminal-retry", "worker")
	request := terminalOrderAcknowledgment(t, queue, leaseID, "worker", true)
	request.RetryAfterNanoseconds = int64(time.Second)
	server := newExchangeServer(queue, nil)

	_, err := server.AckOrder(t.Context(), request)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("terminal retry delay status = %v, want InvalidArgument", status.Code(err))
	}
	if _, found := leaseRecordFor(t, queue, leaseID); !found {
		t.Fatal("rejected terminal retry delay removed the lease")
	}
}
