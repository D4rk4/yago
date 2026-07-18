package crawlorder

import (
	"context"
	"math"
	"testing"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestActiveOrderLeaseRebindingRejectsIncompleteLeaseIdentity(t *testing.T) {
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		frontier.NewFrontier(1, nil),
	)
	claim := consumer.leaseRebinder([]byte("run"))("", "replacement-lease")
	if claim != activeOrderRejected {
		t.Fatalf("incomplete lease rebind claim = %d, want rejected", claim)
	}
}

func TestRecoveredSeedingRejectsAnInvalidPersistedOrder(t *testing.T) {
	terminated := false
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		frontier.NewFrontier(1, nil),
	)
	_, requests, prepared := consumer.prepareRecoveredSeedingOrder(
		t.Context(),
		yagocrawlcontract.CrawlOrder{Requests: []yagocrawlcontract.CrawlRequest{{
			URL: "not-a-crawl-url",
		}}},
		CrawlOrderDelivery{Term: func(context.Context) error {
			terminated = true

			return nil
		}},
	)
	if prepared || requests != nil || !terminated {
		t.Fatalf(
			"invalid recovered order prepared/requests/terminated = %t/%v/%t",
			prepared,
			requests,
			terminated,
		)
	}
}

func TestHeartbeatLeaseConfirmationHandlesCapacityAndTransportFailure(t *testing.T) {
	full := crawllease.NewGrantRegistry(t.Context(), 1)
	if err := full.Track("occupied-lease"); err != nil {
		t.Fatal(err)
	}
	capacityDelivery := heartbeatDelivery{
		client:      &fakeStreamer{ctx: t.Context()},
		leaseGrants: full,
	}
	if capacityDelivery.confirmLease(t.Context(), "overflow-lease") {
		t.Fatal("lease confirmation exceeded registry capacity")
	}

	unavailable := crawllease.NewGrantRegistry(t.Context(), 1)
	transportDelivery := heartbeatDelivery{
		client: &fakeStreamer{
			ctx:     t.Context(),
			beatErr: context.DeadlineExceeded,
		},
		leaseGrants: unavailable,
	}
	if transportDelivery.confirmLease(t.Context(), "transport-lease") {
		t.Fatal("lease survived a failed confirmation heartbeat")
	}
	if active := unavailable.ActiveLeaseIDs(); len(active) != 0 {
		t.Fatalf("failed confirmation left active leases %v", active)
	}
}

func TestHeartbeatLeaseConfirmationAppliesControlBeforeDelivery(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	control := &recordingControlHandler{}
	client := &fakeStreamer{
		ctx:         t.Context(),
		renewActive: true,
		leaseTTL:    time.Hour,
		beatDirectives: []*crawlrpc.CrawlControlDirective{{
			DirectiveId: 91,
			Kind:        crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE,
		}},
	}
	delivery := heartbeatDelivery{
		client:          client,
		workerID:        "worker",
		workerSessionID: "session",
		control:         control,
		acknowledgments: &controlAcknowledgments{},
		leaseGrants:     registry,
	}
	if !delivery.confirmLease(t.Context(), "controlled-lease") {
		t.Fatal("renewed lease was not confirmed")
	}
	if applied := control.snapshot(); len(applied) != 1 || applied[0].DirectiveID != 91 {
		t.Fatalf("applied controls = %+v, want directive 91", applied)
	}
	if !registry.Confirmed("controlled-lease") {
		t.Fatal("control acknowledgment revoked the confirmed lease")
	}
}

type oversizedHeartbeatLeaseClient struct {
	*fakeStreamer
}

func (client oversizedHeartbeatLeaseClient) Heartbeat(
	context.Context,
	*crawlrpc.WorkerHeartbeat,
	...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	return &crawlrpc.WorkerHeartbeatResult{LeaseTtlMilliseconds: math.MaxUint64}, nil
}

func TestHeartbeatRejectsLeaseLifetimeOutsideDurationRange(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	delivery := heartbeatDelivery{
		client: oversizedHeartbeatLeaseClient{
			fakeStreamer: &fakeStreamer{ctx: t.Context()},
		},
		leaseGrants: registry,
	}
	if _, err := delivery.exchange(t.Context(), nil); err == nil {
		t.Fatal("out-of-range heartbeat lease lifetime was accepted")
	}
}

func TestProgressDeliveryRejectsAnUnleasedReportInLeaseMode(t *testing.T) {
	client := &progressDeliveryClient{}
	queue := newProgressDeliveryQueue(
		client,
		"worker",
		testProgressDeliveryPolicy(),
		WithProgressLeaseSession(
			"session",
			crawllease.NewGrantRegistry(t.Context(), 1),
		),
	)
	queue.enqueue(t.Context(), RunReport{
		Provenance: []byte("unleased-run"),
		State:      yagocrawlcontract.CrawlRunFinished,
	})
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close lease-aware progress queue: %v", err)
	}
	if calls, _ := client.snapshot(); len(calls) != 0 {
		t.Fatalf("unleased progress made %d RPC calls", len(calls))
	}
}
