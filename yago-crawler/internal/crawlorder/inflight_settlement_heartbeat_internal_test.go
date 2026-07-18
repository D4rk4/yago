package crawlorder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type inFlightSettlementHeartbeatClient struct {
	*fakeStreamer
	heartbeatSnapshots   chan []string
	dispositionCommitted chan struct{}
	heartbeatFinished    <-chan struct{}
	heartbeatRenewed     []string
	confirmationToken    []byte
	commitOnce           sync.Once
}

func (client *inFlightSettlementHeartbeatClient) Heartbeat(
	ctx context.Context,
	heartbeat *crawlrpc.WorkerHeartbeat,
	_ ...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	snapshot := append([]string(nil), heartbeat.GetActiveLeaseIds()...)
	select {
	case client.heartbeatSnapshots <- snapshot:
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	select {
	case <-client.dispositionCommitted:
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	return &crawlrpc.WorkerHeartbeatResult{
		RenewedLeaseIds:      append([]string(nil), client.heartbeatRenewed...),
		LeaseTtlMilliseconds: uint64(time.Hour / time.Millisecond),
	}, nil
}

func (client *inFlightSettlementHeartbeatClient) AckOrder(
	ctx context.Context,
	request *crawlrpc.OrderAck,
	_ ...grpc.CallOption,
) (*crawlrpc.OrderAckResult, error) {
	if len(request.GetConfirmationToken()) != 0 {
		return &crawlrpc.OrderAckResult{}, nil
	}
	client.commitOnce.Do(func() { close(client.dispositionCommitted) })
	select {
	case <-client.heartbeatFinished:
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	return &crawlrpc.OrderAckResult{
		ConfirmationToken: append([]byte(nil), client.confirmationToken...),
	}, nil
}

func TestOrdinarySettlementSurvivesAlreadyInFlightOmittedHeartbeat(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	leaseID := "ordinary-in-flight"
	confirmTestGrant(t, registry, leaseID)
	grantContext, found := registry.Context(leaseID)
	if !found {
		t.Fatal("ordinary in-flight grant context is missing")
	}
	losses := registry.LeaseLosses()
	streamContext, finishStream := orderStreamAttemptContext(
		t.Context(),
		&heartbeatDelivery{leaseGrants: registry},
	)
	defer finishStream()
	heartbeatFinished := make(chan struct{})
	client := &inFlightSettlementHeartbeatClient{
		fakeStreamer:         &fakeStreamer{ctx: t.Context()},
		heartbeatSnapshots:   make(chan []string, 1),
		dispositionCommitted: make(chan struct{}),
		heartbeatFinished:    heartbeatFinished,
	}
	heartbeat := heartbeatDelivery{
		client:          client,
		workerID:        "worker",
		workerSessionID: "session",
		leaseGrants:     registry,
		operation:       &sync.Mutex{},
	}
	heartbeatDone := make(chan struct{})
	go func() {
		heartbeat.deliver(t.Context())
		close(heartbeatFinished)
		close(heartbeatDone)
	}()
	if snapshot := awaitTargetHeartbeat(t, client.heartbeatSnapshots); !slices.Equal(
		snapshot,
		[]string{leaseID},
	) {
		t.Fatalf("in-flight ordinary heartbeat leases = %v", snapshot)
	}
	delivery := ordinarySettlementDelivery(t, client, registry, leaseID)
	settled := make(chan error, 1)
	go func() { settled <- delivery.Ack(t.Context()) }()
	awaitSettlementRaceCompletion(t, heartbeatDone, settled)
	assertSettledGrantState(t, registry, grantContext, losses, streamContext)
}

func TestRichTerminalSettlementSurvivesAlreadyInFlightOmittedHeartbeat(t *testing.T) {
	checkpoint, settlement := terminalRelayCheckpoint(
		t,
		filepath.Join(t.TempDir(), "frontier.db"),
	)
	defer func() { _ = checkpoint.Close() }()
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	confirmTestGrant(t, registry, settlement.LeaseID)
	grantContext, found := registry.Context(settlement.LeaseID)
	if !found {
		t.Fatal("terminal in-flight grant context is missing")
	}
	losses := registry.LeaseLosses()
	streamContext, finishStream := orderStreamAttemptContext(
		t.Context(),
		&heartbeatDelivery{leaseGrants: registry},
	)
	defer finishStream()
	heartbeatFinished := make(chan struct{})
	client := &inFlightSettlementHeartbeatClient{
		fakeStreamer:         &fakeStreamer{ctx: t.Context()},
		heartbeatSnapshots:   make(chan []string, 1),
		dispositionCommitted: make(chan struct{}),
		heartbeatFinished:    heartbeatFinished,
		confirmationToken:    bytes.Repeat([]byte{0x73}, sha256.Size),
	}
	heartbeat := heartbeatDelivery{
		client:          client,
		workerID:        settlement.WorkerID,
		workerSessionID: settlement.WorkerSessionID,
		leaseGrants:     registry,
		operation:       &sync.Mutex{},
	}
	heartbeatDone := make(chan struct{})
	go func() {
		heartbeat.deliver(t.Context())
		close(heartbeatFinished)
		close(heartbeatDone)
	}()
	if snapshot := awaitTargetHeartbeat(t, client.heartbeatSnapshots); !slices.Equal(
		snapshot,
		[]string{settlement.LeaseID},
	) {
		t.Fatalf("in-flight terminal heartbeat leases = %v", snapshot)
	}
	relay := newTerminalSettlementRelay(client, checkpoint)
	relay.bindWorkerLeaseSession(
		settlement.WorkerID,
		settlement.WorkerSessionID,
		registry,
	)
	settled := make(chan error, 1)
	go func() { settled <- relay.stageAndDeliver(t.Context(), settlement) }()
	awaitSettlementRaceCompletion(t, heartbeatDone, settled)
	assertSettledGrantState(t, registry, grantContext, losses, streamContext)
	awaiting, err := checkpoint.Awaiting(t.Context())
	if err != nil || len(awaiting) != 0 {
		t.Fatalf("completed terminal outbox = %+v, %v", awaiting, err)
	}
}

func TestRecoveredMalformedSettlementSurvivesAlreadyInFlightOmittedHeartbeat(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 2)
	liveLeaseID := "recovered-live"
	malformedLeaseID := "recovered-malformed"
	confirmTestGrant(t, registry, liveLeaseID)
	confirmTestGrant(t, registry, malformedLeaseID)
	grantContext, found := registry.Context(malformedLeaseID)
	if !found {
		t.Fatal("recovered malformed grant context is missing")
	}
	losses := registry.LeaseLosses()
	streamContext, finishStream := orderStreamAttemptContext(
		t.Context(),
		&heartbeatDelivery{leaseGrants: registry},
	)
	defer finishStream()
	heartbeatFinished := make(chan struct{})
	client := &inFlightSettlementHeartbeatClient{
		fakeStreamer:         &fakeStreamer{ctx: t.Context()},
		heartbeatSnapshots:   make(chan []string, 1),
		dispositionCommitted: make(chan struct{}),
		heartbeatFinished:    heartbeatFinished,
		heartbeatRenewed:     []string{liveLeaseID},
	}
	heartbeat := &heartbeatDelivery{
		client:          client,
		workerID:        "worker",
		workerSessionID: "session",
		leaseGrants:     registry,
		operation:       &sync.Mutex{},
	}
	heartbeatDone := make(chan struct{})
	go func() {
		heartbeat.deliver(t.Context())
		close(heartbeatFinished)
		close(heartbeatDone)
	}()
	if snapshot := awaitTargetHeartbeat(t, client.heartbeatSnapshots); !slices.Equal(
		snapshot,
		[]string{liveLeaseID, malformedLeaseID},
	) {
		t.Fatalf("in-flight recovered heartbeat leases = %v", snapshot)
	}
	received := receiveRecoveredMalformedOrder(
		t,
		client,
		heartbeat,
		liveLeaseID,
		malformedLeaseID,
	)
	select {
	case <-heartbeatDone:
	case <-time.After(time.Second):
		t.Fatal("omitted recovered heartbeat did not finish")
	}
	select {
	case accepted := <-received:
		if !accepted {
			t.Fatal("settled recovered malformed order stopped the stream")
		}
	case <-time.After(time.Second):
		t.Fatal("recovered malformed settlement did not finish")
	}
	if !registry.Confirmed(liveLeaseID) || registry.Confirmed(malformedLeaseID) {
		t.Fatalf("recovered grant state = %v", registry.ActiveLeaseIDs())
	}
	select {
	case <-grantContext.Done():
	case <-time.After(time.Second):
		t.Fatal("recovered malformed grant context remained active")
	}
	if cause := context.Cause(grantContext); !errors.Is(cause, context.Canceled) ||
		errors.Is(cause, crawllease.ErrLeaseLost) {
		t.Fatalf("recovered malformed grant context cause = %v", cause)
	}
	assertLeaseLossQuiet(t, losses, streamContext)
}

func receiveRecoveredMalformedOrder(
	t *testing.T,
	client OrderStreamer,
	heartbeat *heartbeatDelivery,
	liveLeaseID string,
	malformedLeaseID string,
) <-chan bool {
	t.Helper()
	recovered := recoveredOrderReplay{
		leaseIDs: []string{liveLeaseID, malformedLeaseID},
		next:     1,
	}
	received := make(chan bool, 1)
	go func() {
		received <- receiveRecoveredOrder(
			t.Context(),
			crawlOrderStreamDrain{
				client: client, out: make(chan CrawlOrderDelivery, 1),
				workerID: "worker", heartbeat: heartbeat,
			},
			&recovered,
			&crawlrpc.CrawlOrderMessage{
				OrderJson: []byte("{"), LeaseId: malformedLeaseID,
				Recovered: true, RecoveredBatchEnd: true,
			},
		)
	}()

	return received
}

func awaitSettlementRaceCompletion(
	t *testing.T,
	heartbeatDone <-chan struct{},
	settled <-chan error,
) {
	t.Helper()
	select {
	case <-heartbeatDone:
	case <-time.After(time.Second):
		t.Fatal("omitted in-flight heartbeat did not finish")
	}
	select {
	case err := <-settled:
		if err != nil {
			t.Fatalf("settlement after omitted in-flight heartbeat: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("settlement response did not finish")
	}
}
