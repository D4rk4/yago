package crawlorder

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestWorkerHeartbeatPreservesAbsentAndZeroActiveFetches(t *testing.T) {
	absent := workerHeartbeat("legacy", nil, 3, 5)
	if absent.ActiveFetches != nil {
		t.Fatalf("absent active fetches = %v, want nil", absent.ActiveFetches)
	}
	if got := absent.GetAcknowledgedDirectiveIds(); len(got) != 2 || got[0] != 3 || got[1] != 5 {
		t.Fatalf("acknowledged directive ids = %v, want [3 5]", got)
	}

	zero := workerHeartbeat("current", func() uint32 { return 0 })
	if zero.ActiveFetches == nil || zero.GetActiveFetches() != 0 {
		t.Fatalf(
			"zero active fetches = %v/%d, want present zero",
			zero.ActiveFetches,
			zero.GetActiveFetches(),
		)
	}
}

func TestWorkerHeartbeatPreservesAbsentAndZeroAdmissionWaiters(t *testing.T) {
	absent := workerHeartbeat("legacy", nil)
	if absent.FetchAdmissionWaiting != nil {
		t.Fatalf("absent admission waiters = %v, want nil", absent.FetchAdmissionWaiting)
	}
	zero := workerHeartbeatWithWaiters(
		"current",
		func() uint32 { return 0 },
		func() uint32 { return 0 },
	)
	if zero.FetchAdmissionWaiting == nil || zero.GetFetchAdmissionWaiting() != 0 {
		t.Fatalf("zero admission waiters = %v, want present zero", zero.FetchAdmissionWaiting)
	}
}

func TestGRPCOrderReceiverReportsLiveActiveFetches(t *testing.T) {
	fastHeartbeat(t)
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamer{ctx: ctx}
	var active atomic.Uint32
	var waiting atomic.Uint32
	active.Store(3)
	waiting.Store(2)
	receiver := NewGRPCOrderReceiver(
		ctx,
		client,
		"worker-active",
		nil,
		WithHeartbeatActiveFetches(active.Load),
		WithHeartbeatFetchAdmissionWaiters(waiting.Load),
	)
	requests := client.heartbeatRequests()
	if len(requests) < 1 || requests[0].ActiveFetches == nil ||
		requests[0].GetActiveFetches() != 3 || requests[0].FetchAdmissionWaiting == nil ||
		requests[0].GetFetchAdmissionWaiting() != 2 {
		t.Fatalf("startup heartbeats = %+v, want present active count 3", requests)
	}

	active.Store(0)
	deadline := time.After(2 * time.Second)
	for !hasHeartbeatActiveFetches(client.heartbeatRequests(), 0) {
		select {
		case <-deadline:
			t.Fatal("periodic active-fetch heartbeat did not arrive")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	drainUntilClosed(t, receiver)
}

func hasHeartbeatActiveFetches(requests []*crawlrpc.WorkerHeartbeat, want uint32) bool {
	for _, request := range requests {
		if request.ActiveFetches != nil && request.GetActiveFetches() == want {
			return true
		}
	}

	return false
}
