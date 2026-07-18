package crawlorder

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
)

func TestSuccessfulOrdinarySettlementsReleaseGrantWithoutStreamLoss(t *testing.T) {
	for _, test := range []struct {
		name        string
		settle      func(CrawlOrderDelivery) func(context.Context) error
		requeue     bool
		acknowledge []error
	}{
		{
			name:        "ack retry",
			settle:      func(delivery CrawlOrderDelivery) func(context.Context) error { return delivery.Ack },
			acknowledge: []error{status.Error(codes.Unavailable, "response lost")},
		},
		{
			name:    "nak",
			settle:  func(delivery CrawlOrderDelivery) func(context.Context) error { return delivery.Nak },
			requeue: true,
		},
		{
			name:   "legacy term",
			settle: func(delivery CrawlOrderDelivery) func(context.Context) error { return delivery.Term },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := crawllease.NewGrantRegistry(t.Context(), 1)
			leaseID := "ordinary-" + test.name
			confirmTestGrant(t, registry, leaseID)
			grantContext, found := registry.Context(leaseID)
			if !found {
				t.Fatal("ordinary settlement grant context is missing")
			}
			losses := registry.LeaseLosses()
			streamContext, finishStream := orderStreamAttemptContext(
				t.Context(),
				&heartbeatDelivery{leaseGrants: registry},
			)
			defer finishStream()
			client := &fakeStreamer{ctx: t.Context(), ackErrors: test.acknowledge}
			delivery := ordinarySettlementDelivery(t, client, registry, leaseID)
			settle := test.settle(delivery)
			if err := settle(t.Context()); err != nil {
				t.Fatalf("settle ordinary grant: %v", err)
			}
			if err := settle(t.Context()); err != nil {
				t.Fatalf("repeat ordinary settlement: %v", err)
			}
			assertSettledGrantState(t, registry, grantContext, losses, streamContext)
			for index, call := range client.acknowledgementCalls() {
				if call.GetRequeue() != test.requeue {
					t.Fatalf("settlement call %d requeue = %v", index, call.GetRequeue())
				}
			}
		})
	}
}

func TestFailedOrdinarySettlementRetainsGrant(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	leaseID := "ordinary-failure"
	confirmTestGrant(t, registry, leaseID)
	losses := registry.LeaseLosses()
	streamContext, finishStream := orderStreamAttemptContext(
		t.Context(),
		&heartbeatDelivery{leaseGrants: registry},
	)
	defer finishStream()
	client := &fakeStreamer{
		ctx: t.Context(),
		ackErrors: []error{
			status.Error(codes.Unavailable, "temporary failure"),
			status.Error(codes.InvalidArgument, "final failure"),
		},
	}
	delivery := ordinarySettlementDelivery(t, client, registry, leaseID)
	if err := delivery.Ack(t.Context()); err == nil {
		t.Fatal("failed ordinary settlement succeeded")
	}
	if !registry.Confirmed(leaseID) {
		t.Fatal("failed ordinary settlement released its grant")
	}
	assertLeaseLossQuiet(t, losses, streamContext)
}

func TestSuccessfulOrdinarySettlementRemovesUnconfirmedGrant(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	leaseID := "ordinary-unconfirmed"
	if err := registry.Track(leaseID); err != nil {
		t.Fatal(err)
	}
	losses := registry.LeaseLosses()
	client := &fakeStreamer{ctx: t.Context()}
	delivery := ordinarySettlementDelivery(t, client, registry, leaseID)
	if err := delivery.Ack(t.Context()); err != nil {
		t.Fatalf("settle unconfirmed grant: %v", err)
	}
	if active := registry.ActiveLeaseIDs(); len(active) != 0 {
		t.Fatalf("settled unconfirmed grant remained active: %v", active)
	}
	select {
	case <-losses:
		t.Fatal("unconfirmed settlement emitted lease loss")
	default:
	}
}

func ordinarySettlementDelivery(
	t *testing.T,
	client OrderStreamer,
	registry *crawllease.GrantRegistry,
	leaseID string,
) CrawlOrderDelivery {
	t.Helper()
	out := make(chan CrawlOrderDelivery, 1)
	if !deliverOrderWithLeaseSession(t.Context(), crawlOrderDeliveryEnvelope{
		client:   client,
		out:      out,
		leaseID:  leaseID,
		workerID: "worker",
		heartbeat: &heartbeatDelivery{
			workerSessionID: "session",
			leaseGrants:     registry,
		},
	}) {
		t.Fatal("ordinary settlement delivery was not emitted")
	}

	return <-out
}

func assertSettledGrantState(
	t *testing.T,
	registry *crawllease.GrantRegistry,
	grantContext context.Context,
	losses <-chan struct{},
	streamContext context.Context,
) {
	t.Helper()
	if active := registry.ActiveLeaseIDs(); len(active) != 0 {
		t.Fatalf("settled lease remained active: %v", active)
	}
	select {
	case <-grantContext.Done():
	case <-time.After(time.Second):
		t.Fatal("settled grant context remained active")
	}
	if cause := context.Cause(grantContext); !errors.Is(cause, context.Canceled) ||
		errors.Is(cause, crawllease.ErrLeaseLost) {
		t.Fatalf("settled grant context cause = %v", cause)
	}
	assertLeaseLossQuiet(t, losses, streamContext)
}

func assertLeaseLossQuiet(
	t *testing.T,
	losses <-chan struct{},
	streamContext context.Context,
) {
	t.Helper()
	select {
	case <-losses:
		t.Fatal("intentional settlement emitted lease loss")
	default:
	}
	if err := streamContext.Err(); err != nil {
		t.Fatalf("intentional settlement cancelled the healthy stream: %v", err)
	}
}
