package crawlorder

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type failingLeaseSettlementEntropy struct{}

func (failingLeaseSettlementEntropy) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestSettleLeaseRetriesAckNakAndTerm(t *testing.T) {
	policy := leaseSettlementPolicy{
		retryWait:        time.Millisecond,
		maximumRetryWait: time.Millisecond,
		shutdownWait:     50 * time.Millisecond,
	}
	for _, test := range []struct {
		name    string
		requeue bool
	}{
		{name: "ack"},
		{name: "nak", requeue: true},
		{name: "term"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeStreamer{
				ctx: context.Background(),
				ackErrors: []error{
					status.Error(codes.Unavailable, "response lost"),
				},
			}
			err := settleLeaseWithPolicy(
				context.Background(),
				client,
				"lease-"+test.name,
				test.requeue,
				policy,
			)(context.Background())
			if err != nil {
				t.Fatalf("settle after transient failure: %v", err)
			}
			calls := client.acknowledgementCalls()
			if len(calls) != 2 {
				t.Fatalf("acknowledgement calls = %d, want 2", len(calls))
			}
			if calls[0].GetRequeue() != test.requeue || calls[1].GetRequeue() != test.requeue {
				t.Fatalf("requeue flags = %v/%v, want %v",
					calls[0].GetRequeue(), calls[1].GetRequeue(), test.requeue)
			}
			if len(client.ackedLeases()) != 1 {
				t.Fatal("successful retry was not recorded exactly once")
			}
		})
	}
}

func TestSettleLeaseStopsAfterDetachedShutdownDeadline(t *testing.T) {
	serviceCtx, cancelService := context.WithCancel(context.Background())
	client := &fakeStreamer{
		ctx:    context.Background(),
		ackErr: status.Error(codes.Unavailable, "node unavailable"),
	}
	policy := leaseSettlementPolicy{
		retryWait:        time.Millisecond,
		maximumRetryWait: 2 * time.Millisecond,
		shutdownWait:     25 * time.Millisecond,
	}
	settled := make(chan error, 1)
	go func() {
		settled <- settleLeaseWithPolicy(
			serviceCtx,
			client,
			"lease-shutdown",
			true,
			policy,
		)(context.Background())
	}()
	deadline := time.After(time.Second)
	for len(client.acknowledgementCalls()) == 0 {
		select {
		case <-deadline:
			t.Fatal("settlement RPC was not attempted")
		case <-time.After(time.Millisecond):
		}
	}
	cancelService()
	select {
	case err := <-settled:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("settlement error = %v, want detached deadline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("settlement retry did not stop after shutdown deadline")
	}
	calls := len(client.acknowledgementCalls())
	time.Sleep(10 * time.Millisecond)
	if later := len(client.acknowledgementCalls()); later != calls {
		t.Fatalf("acknowledgement calls grew after return: %d to %d", calls, later)
	}
}

func TestSettleLeaseDetachedShutdownCanSucceed(t *testing.T) {
	serviceCtx, cancelService := context.WithCancel(context.Background())
	cancelService()
	client := &fakeStreamer{
		ctx: context.Background(),
		ackErrors: []error{
			status.Error(codes.Unavailable, "first shutdown attempt lost"),
		},
	}
	err := settleLeaseWithPolicy(
		serviceCtx,
		client,
		"lease-detached",
		false,
		leaseSettlementPolicy{
			retryWait:        time.Millisecond,
			maximumRetryWait: time.Millisecond,
			shutdownWait:     50 * time.Millisecond,
		},
	)(context.Background())
	if err != nil {
		t.Fatalf("detached settlement: %v", err)
	}
	if calls := len(client.acknowledgementCalls()); calls != 2 {
		t.Fatalf("detached acknowledgement calls = %d, want 2", calls)
	}
}

func TestLeaseSettlementRetryWaitStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamer{
		ctx:    context.Background(),
		ackErr: status.Error(codes.Unavailable, "node unavailable"),
	}
	settled := make(chan error, 1)
	go func() {
		settled <- (leaseSettlementSession{
			client:  client,
			request: &crawlrpc.OrderAck{LeaseId: "lease-cancel-wait"},
			policy: leaseSettlementPolicy{
				retryWait:        time.Hour,
				maximumRetryWait: time.Hour,
			},
		}).retry(ctx)
	}()
	deadline := time.After(time.Second)
	for len(client.acknowledgementCalls()) == 0 {
		select {
		case <-deadline:
			t.Fatal("settlement RPC was not attempted")
		case <-time.After(time.Millisecond):
		}
	}
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-settled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("settlement error = %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("settlement wait did not stop on cancellation")
	}
}

func TestSettleLeaseRejectsEmptyLeaseWithoutRPC(t *testing.T) {
	client := &fakeStreamer{ctx: context.Background()}
	if err := settleLease(
		context.Background(),
		client,
		"",
		false,
	)(context.Background()); err == nil {
		t.Fatal("empty lease settlement succeeded")
	}
	if calls := len(client.acknowledgementCalls()); calls != 0 {
		t.Fatalf("empty lease acknowledgement calls = %d, want 0", calls)
	}
}

func TestLeaseSettlementRetryPolicyBounds(t *testing.T) {
	for _, code := range []codes.Code{
		codes.Canceled,
		codes.DeadlineExceeded,
		codes.Unknown,
		codes.ResourceExhausted,
		codes.Aborted,
		codes.Internal,
		codes.Unavailable,
	} {
		if !retryableLeaseSettlementStatus(code) {
			t.Fatalf("status %s is not retryable", code)
		}
	}
	if retryableLeaseSettlementStatus(codes.InvalidArgument) {
		t.Fatal("invalid argument settlement is retryable")
	}
	wait := 20 * time.Millisecond
	jittered := jitteredLeaseSettlementRetryWait(wait, bytes.NewReader(make([]byte, 8)))
	if jittered < wait/2 || jittered >= wait {
		t.Fatalf("jittered wait = %v, want [%v, %v)", jittered, wait/2, wait)
	}
	if fallback := jitteredLeaseSettlementRetryWait(
		wait,
		failingLeaseSettlementEntropy{},
	); fallback != wait/2 {
		t.Fatalf("entropy failure wait = %v, want %v", fallback, wait/2)
	}
}
