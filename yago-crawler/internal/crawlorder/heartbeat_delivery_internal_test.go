package crawlorder

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestHeartbeatDeliveryAcknowledgesAppliedDirective(t *testing.T) {
	client := &fakeStreamer{
		ctx: context.Background(),
		beatDirectives: []*crawlrpc.CrawlControlDirective{{
			DirectiveId: 17,
			Kind:        crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE,
		}},
	}
	handler := &recordingControlHandler{}
	acknowledgments := &controlAcknowledgments{}
	delivery := heartbeatDelivery{
		client:          client,
		workerID:        "worker",
		control:         handler,
		acknowledgments: acknowledgments,
	}
	delivery.deliver(t.Context())

	requests := client.heartbeatRequests()
	if len(requests) != 2 || len(requests[0].GetAcknowledgedDirectiveIds()) != 0 ||
		len(requests[1].GetAcknowledgedDirectiveIds()) != 1 ||
		requests[1].GetAcknowledgedDirectiveIds()[0] != 17 {
		t.Fatalf("heartbeat requests = %+v, want delivery then acknowledgment 17", requests)
	}
	if applied := handler.snapshot(); len(applied) != 1 || applied[0].DirectiveID != 17 {
		t.Fatalf("applied directives = %+v, want identity 17", applied)
	}
	if pending := acknowledgments.snapshot(); len(pending) != 0 {
		t.Fatalf("pending acknowledgments = %v, want empty", pending)
	}
}

type serializedHeartbeatClient struct {
	*fakeStreamer
	mu        sync.Mutex
	active    int
	maximum   int
	entered   chan struct{}
	continued chan struct{}
}

func (c *serializedHeartbeatClient) Heartbeat(
	context.Context,
	*crawlrpc.WorkerHeartbeat,
	...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	c.mu.Lock()
	c.active++
	c.maximum = max(c.maximum, c.active)
	c.mu.Unlock()
	c.entered <- struct{}{}
	<-c.continued
	c.mu.Lock()
	c.active--
	c.mu.Unlock()

	return &crawlrpc.WorkerHeartbeatResult{}, nil
}

func TestHeartbeatOperationsAreSerialized(t *testing.T) {
	client := &serializedHeartbeatClient{
		fakeStreamer: &fakeStreamer{ctx: t.Context()},
		entered:      make(chan struct{}, 2),
		continued:    make(chan struct{}, 2),
	}
	delivery := heartbeatDelivery{client: client, workerID: "worker", operation: &sync.Mutex{}}
	done := make(chan struct{}, 2)
	go func() { delivery.deliver(t.Context()); done <- struct{}{} }()
	go func() { delivery.deliver(t.Context()); done <- struct{}{} }()
	select {
	case <-client.entered:
	case <-time.After(time.Second):
		t.Fatal("first heartbeat did not start")
	}
	select {
	case <-client.entered:
		t.Fatal("second heartbeat overlapped the first")
	case <-time.After(25 * time.Millisecond):
	}
	client.continued <- struct{}{}
	select {
	case <-client.entered:
	case <-time.After(time.Second):
		t.Fatal("second heartbeat did not start after release")
	}
	client.continued <- struct{}{}
	for range 2 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("serialized heartbeat did not finish")
		}
	}
	client.mu.Lock()
	maximum := client.maximum
	client.mu.Unlock()
	if maximum != 1 {
		t.Fatalf("maximum concurrent heartbeats = %d, want 1", maximum)
	}
}

func TestHeartbeatDoesNotApplyDirectivesBeyondAcknowledgmentCapacity(t *testing.T) {
	directives := make(
		[]*crawlrpc.CrawlControlDirective,
		yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments+17,
	)
	for index := range directives {
		directives[index] = &crawlrpc.CrawlControlDirective{
			DirectiveId: uint64(index + 1),
			Kind:        crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_RESTART,
		}
	}
	acknowledgments := &controlAcknowledgments{}
	handler := &recordingControlHandler{}
	delivery := heartbeatDelivery{control: handler, acknowledgments: acknowledgments}
	applied := delivery.dispatchDirectives(t.Context(), &crawlrpc.WorkerHeartbeatResult{
		Directives: directives,
	})
	acknowledgments.add(applied)
	if len(applied) != yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments ||
		len(handler.snapshot()) != yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments ||
		len(acknowledgments.snapshot()) !=
			yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments {
		t.Fatalf(
			"applied/handled/pending = %d/%d/%d",
			len(applied), len(handler.snapshot()), len(acknowledgments.snapshot()),
		)
	}
}

func TestHeartbeatDeliveryRetriesUnconfirmedAcknowledgment(t *testing.T) {
	client := &fakeStreamer{
		ctx:        context.Background(),
		beatErrors: []error{nil, errors.New("ack unavailable")},
		beatDirectives: []*crawlrpc.CrawlControlDirective{{
			DirectiveId: 23,
			Kind:        crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL,
		}},
	}
	acknowledgments := &controlAcknowledgments{}
	delivery := heartbeatDelivery{
		client:          client,
		workerID:        "worker",
		control:         &recordingControlHandler{},
		acknowledgments: acknowledgments,
	}
	delivery.deliver(t.Context())
	if pending := acknowledgments.snapshot(); len(pending) != 1 || pending[0] != 23 {
		t.Fatalf("pending acknowledgments = %v, want [23]", pending)
	}

	client.mu.Lock()
	client.beatDirectives = nil
	client.mu.Unlock()
	delivery.deliver(t.Context())
	requests := client.heartbeatRequests()
	if len(requests) != 3 || len(requests[2].GetAcknowledgedDirectiveIds()) != 1 ||
		requests[2].GetAcknowledgedDirectiveIds()[0] != 23 {
		t.Fatalf("retry requests = %+v, want acknowledgment 23 on third call", requests)
	}
	if pending := acknowledgments.snapshot(); len(pending) != 0 {
		t.Fatalf("pending acknowledgments after retry = %v, want empty", pending)
	}
}

func TestHeartbeatFailedPreconditionRevokesActiveLeases(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	if err := registry.Track("lease"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	registry.Renew(started, time.Hour, []string{"lease"}, []string{"lease"})
	leaseContext, ok := registry.Context("lease")
	if !ok {
		t.Fatal("confirmed lease context missing")
	}
	delivery := heartbeatDelivery{
		client: &fakeStreamer{
			ctx:     context.Background(),
			beatErr: status.Error(codes.FailedPrecondition, "session superseded"),
		},
		workerID: "worker", workerSessionID: "session", leaseGrants: registry,
	}
	delivery.deliver(t.Context())
	select {
	case <-leaseContext.Done():
	case <-time.After(time.Second):
		t.Fatal("failed-precondition heartbeat did not revoke active lease")
	}
	if !errors.Is(context.Cause(leaseContext), crawllease.ErrLeaseLost) {
		t.Fatalf("lease context cause = %v", context.Cause(leaseContext))
	}
}
