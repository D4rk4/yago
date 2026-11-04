package crawlorder

import (
	"context"
	"testing"
	"time"
)

func TestLeaseSessionDeliverySendsRequestedRetryDelay(t *testing.T) {
	client := &fakeStreamer{ctx: context.Background()}
	delivery := leaseSessionCrawlOrderDelivery(t.Context(), crawlOrderDeliveryEnvelope{
		client:    client,
		leaseID:   "retry-lease",
		workerID:  "worker",
		heartbeat: &heartbeatDelivery{workerSessionID: "worker-session"},
	})
	retryAfter := 17 * time.Minute
	if err := delivery.NakAfter(t.Context(), retryAfter); err != nil {
		t.Fatalf("defer leased order: %v", err)
	}
	calls := client.acknowledgementCalls()
	if len(calls) != 1 || calls[0].GetLeaseId() != "retry-lease" ||
		!calls[0].GetRequeue() || calls[0].GetRetryAfterNanoseconds() != int64(retryAfter) ||
		calls[0].GetWorkerId() != "worker" ||
		calls[0].GetWorkerSessionId() != "worker-session" {
		t.Fatalf("retry acknowledgment calls = %+v", calls)
	}
}
