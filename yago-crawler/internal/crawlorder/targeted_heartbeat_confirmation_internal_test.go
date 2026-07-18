package crawlorder

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestRecoveredLeaseConfirmationTargetsOnlyCurrentBatch(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 3)
	confirmTestGrant(t, registry, "existing")
	client := &fakeStreamer{
		ctx:         t.Context(),
		renewActive: true,
		leaseTTL:    time.Minute,
		beatDirectives: []*crawlrpc.CrawlControlDirective{{
			DirectiveId: 91,
			Kind:        crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE,
		}},
	}
	delivery := heartbeatDelivery{
		client:          client,
		workerID:        "worker",
		workerSessionID: "session",
		control:         &recordingControlHandler{},
		acknowledgments: &controlAcknowledgments{},
		leaseGrants:     registry,
		operation:       &sync.Mutex{},
	}
	batch := []string{"batch-a", "batch-b"}
	if !delivery.confirmRecoveredLeases(t.Context(), batch) {
		t.Fatal("recovered lease batch was not confirmed")
	}
	requests := client.heartbeatRequests()
	if len(requests) != 2 {
		t.Fatalf("recovered confirmation heartbeats = %d, want 2", len(requests))
	}
	for index, request := range requests {
		if !slices.Equal(request.GetActiveLeaseIds(), batch) {
			t.Fatalf(
				"recovered confirmation heartbeat %d leases = %v, want %v",
				index,
				request.GetActiveLeaseIds(),
				batch,
			)
		}
	}
	if acknowledged := requests[1].GetAcknowledgedDirectiveIds(); len(acknowledged) != 1 ||
		acknowledged[0] != 91 {
		t.Fatalf("recovered directive acknowledgment = %v, want [91]", acknowledged)
	}
	if active := registry.ActiveLeaseIDs(); !slices.Equal(
		active,
		[]string{"batch-a", "batch-b", "existing"},
	) {
		t.Fatalf("active leases after recovered confirmation = %v", active)
	}
}

type gatedTargetHeartbeatClient struct {
	*fakeStreamer
	requests chan []string
	release  chan struct{}
}

func (c *gatedTargetHeartbeatClient) Heartbeat(
	ctx context.Context,
	heartbeat *crawlrpc.WorkerHeartbeat,
	_ ...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	leaseIDs := append([]string(nil), heartbeat.GetActiveLeaseIds()...)
	select {
	case c.requests <- leaseIDs:
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	select {
	case <-c.release:
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	return &crawlrpc.WorkerHeartbeatResult{
		RenewedLeaseIds:      leaseIDs,
		LeaseTtlMilliseconds: uint64(time.Hour / time.Millisecond),
	}, nil
}

func TestTargetedLeaseConfirmationPrecedesFullPeriodicHeartbeat(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 2)
	confirmTestGrant(t, registry, "existing")
	client := &gatedTargetHeartbeatClient{
		fakeStreamer: &fakeStreamer{ctx: t.Context()},
		requests:     make(chan []string, 2),
		release:      make(chan struct{}),
	}
	operation := &observedHeartbeatOperation{attempted: make(chan struct{}, 2)}
	delivery := heartbeatDelivery{
		client:          client,
		workerID:        "worker",
		workerSessionID: "session",
		leaseGrants:     registry,
		operation:       operation,
	}
	confirmed := make(chan bool, 1)
	go func() { confirmed <- delivery.confirmLease(t.Context(), "new") }()
	awaitHeartbeatOperationAttempt(t, operation.attempted)
	if request := awaitTargetHeartbeat(
		t,
		client.requests,
	); !slices.Equal(
		request,
		[]string{"new"},
	) {
		t.Fatalf("targeted confirmation leases = %v, want [new]", request)
	}
	periodicDone := make(chan struct{})
	go func() {
		delivery.deliver(t.Context())
		close(periodicDone)
	}()
	awaitHeartbeatOperationAttempt(t, operation.attempted)
	client.release <- struct{}{}
	select {
	case ok := <-confirmed:
		if !ok {
			t.Fatal("targeted lease confirmation failed")
		}
	case <-time.After(time.Second):
		t.Fatal("targeted lease confirmation did not finish")
	}
	if request := awaitTargetHeartbeat(
		t,
		client.requests,
	); !slices.Equal(
		request,
		[]string{"existing", "new"},
	) {
		t.Fatalf("periodic heartbeat leases = %v, want full active set", request)
	}
	client.release <- struct{}{}
	select {
	case <-periodicDone:
	case <-time.After(time.Second):
		t.Fatal("periodic heartbeat did not finish")
	}
}

func TestTargetedHeartbeatSessionRejectionRevokesEveryGrant(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 2)
	confirmTestGrant(t, registry, "existing")
	existingContext, found := registry.Context("existing")
	if !found {
		t.Fatal("existing grant context is missing")
	}
	delivery := heartbeatDelivery{
		client: &fakeStreamer{
			ctx:     t.Context(),
			beatErr: status.Error(codes.FailedPrecondition, "session superseded"),
		},
		workerID:        "worker",
		workerSessionID: "session",
		leaseGrants:     registry,
	}
	if delivery.confirmLease(t.Context(), "new") {
		t.Fatal("superseded worker session confirmed a new lease")
	}
	select {
	case <-existingContext.Done():
	case <-time.After(time.Second):
		t.Fatal("session rejection did not revoke the existing grant")
	}
	if !errors.Is(context.Cause(existingContext), crawllease.ErrLeaseLost) {
		t.Fatalf("existing grant cause = %v", context.Cause(existingContext))
	}
	if active := registry.ActiveLeaseIDs(); len(active) != 0 {
		t.Fatalf("session rejection retained active leases %v", active)
	}
}

func confirmTestGrant(t *testing.T, registry *crawllease.GrantRegistry, leaseID string) {
	t.Helper()
	if err := registry.Track(leaseID); err != nil {
		t.Fatalf("track lease %q: %v", leaseID, err)
	}
	started := time.Now()
	registry.Renew(started, time.Hour, []string{leaseID}, []string{leaseID})
}

func awaitTargetHeartbeat(t *testing.T, requests <-chan []string) []string {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(time.Second):
		t.Fatal("heartbeat request was not delivered")

		return nil
	}
}
