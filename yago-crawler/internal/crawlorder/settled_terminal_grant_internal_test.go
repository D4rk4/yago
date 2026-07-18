package crawlorder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type delayedTerminalConfirmationClient struct {
	*fakeStreamer
	token               []byte
	confirmationStarted chan struct{}
	releaseConfirmation <-chan struct{}
	startedOnce         sync.Once
}

func (client *delayedTerminalConfirmationClient) AckOrder(
	ctx context.Context,
	request *crawlrpc.OrderAck,
	_ ...grpc.CallOption,
) (*crawlrpc.OrderAckResult, error) {
	if len(request.GetConfirmationToken()) == 0 {
		return &crawlrpc.OrderAckResult{
			ConfirmationToken: append([]byte(nil), client.token...),
		}, nil
	}
	client.startedOnce.Do(func() { close(client.confirmationStarted) })
	select {
	case <-client.releaseConfirmation:
		return nil, status.Error(codes.InvalidArgument, "confirmation rejected")
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

func TestRichTerminalGrantSettlesBeforeDelayedConfirmation(t *testing.T) {
	checkpoint, settlement := terminalRelayCheckpoint(
		t,
		filepath.Join(t.TempDir(), "frontier.db"),
	)
	defer func() { _ = checkpoint.Close() }()
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	confirmTestGrant(t, registry, settlement.LeaseID)
	grantContext, found := registry.Context(settlement.LeaseID)
	if !found {
		t.Fatal("terminal settlement grant context is missing")
	}
	losses := registry.LeaseLosses()
	streamContext, finishStream := orderStreamAttemptContext(
		t.Context(),
		&heartbeatDelivery{leaseGrants: registry},
	)
	defer finishStream()
	releaseConfirmation := make(chan struct{})
	client := &delayedTerminalConfirmationClient{
		fakeStreamer:        &fakeStreamer{ctx: t.Context()},
		token:               bytes.Repeat([]byte{0x71}, sha256.Size),
		confirmationStarted: make(chan struct{}),
		releaseConfirmation: releaseConfirmation,
	}
	relay := newTerminalSettlementRelay(client, checkpoint)
	relay.bindWorkerLeaseSession(
		settlement.WorkerID,
		settlement.WorkerSessionID,
		registry,
	)
	settled := make(chan error, 1)
	go func() { settled <- relay.stageAndDeliver(t.Context(), settlement) }()
	select {
	case <-client.confirmationStarted:
	case <-time.After(time.Second):
		t.Fatal("terminal confirmation did not start")
	}
	assertSettledGrantState(
		t,
		registry,
		grantContext,
		losses,
		streamContext,
	)
	awaiting, err := checkpoint.Awaiting(t.Context())
	if err != nil || len(awaiting) != 1 || awaiting[0].Phase != crawlsettlement.Confirming {
		t.Fatalf("durable terminal confirmation state = %+v, %v", awaiting, err)
	}
	close(releaseConfirmation)
	select {
	case err := <-settled:
		if err == nil {
			t.Fatal("rejected terminal confirmation succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("rejected terminal confirmation did not return")
	}
	if active := registry.ActiveLeaseIDs(); len(active) != 0 {
		t.Fatalf("failed terminal confirmation restored grants %v", active)
	}
	assertLeaseLossQuiet(t, losses, streamContext)
}

func TestRichTerminalGrantRetainedUntilTokenIsDurable(t *testing.T) {
	for _, test := range []struct {
		name   string
		client OrderStreamer
		outbox crawlsettlement.Outbox
	}{
		{
			name: "first phase rejected",
			client: &fakeStreamer{
				ctx:    t.Context(),
				ackErr: status.Error(codes.InvalidArgument, "settlement rejected"),
			},
			outbox: &terminalSettlementOutboxScenario{},
		},
		{
			name: "token persistence failed",
			client: &terminalAcknowledgmentClient{
				token: bytes.Repeat([]byte{0x72}, sha256.Size),
			},
			outbox: &terminalSettlementOutboxScenario{
				recordError: errors.New("token write failed"),
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			settlement := terminalSettlementScenarioDefinition()
			registry := crawllease.NewGrantRegistry(t.Context(), 1)
			confirmTestGrant(t, registry, settlement.LeaseID)
			losses := registry.LeaseLosses()
			streamContext, finishStream := orderStreamAttemptContext(
				t.Context(),
				&heartbeatDelivery{leaseGrants: registry},
			)
			defer finishStream()
			relay := newTerminalSettlementRelay(test.client, test.outbox)
			relay.bindWorkerLeaseSession(
				settlement.WorkerID,
				settlement.WorkerSessionID,
				registry,
			)
			if err := relay.stageAndDeliver(t.Context(), settlement); err == nil {
				t.Fatal("terminal settlement fault succeeded")
			}
			if !registry.Confirmed(settlement.LeaseID) {
				t.Fatal("terminal grant was released before token durability")
			}
			assertLeaseLossQuiet(t, losses, streamContext)
		})
	}
}

func TestDurableTerminalPhaseSettlesGrantBeforeConfirmationReplay(t *testing.T) {
	settlement := terminalSettlementScenarioDefinition()
	settlement.Phase = crawlsettlement.Confirming
	settlement.ConfirmationToken = bytes.Repeat([]byte{0x74}, sha256.Size)
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	confirmTestGrant(t, registry, settlement.LeaseID)
	losses := registry.LeaseLosses()
	outbox := &terminalSettlementOutboxScenario{settlement: settlement}
	relay := newTerminalSettlementRelay(&terminalAcknowledgmentClient{}, outbox)
	relay.bindWorkerLeaseSession(
		settlement.WorkerID,
		settlement.WorkerSessionID,
		registry,
	)
	grantAbsentDuringConfirmation := false
	err := relay.advanceWithDelivery(
		t.Context(),
		settlement,
		func(
			context.Context,
			crawlsettlement.Settlement,
			[]byte,
		) (*crawlrpc.OrderAckResult, error) {
			grantAbsentDuringConfirmation = len(registry.ActiveLeaseIDs()) == 0

			return &crawlrpc.OrderAckResult{}, nil
		},
	)
	if err != nil {
		t.Fatalf("confirm durable terminal settlement: %v", err)
	}
	if !grantAbsentDuringConfirmation || !outbox.completeCalled {
		t.Fatalf(
			"grant absent/completed = %t/%t",
			grantAbsentDuringConfirmation,
			outbox.completeCalled,
		)
	}
	select {
	case <-losses:
		t.Fatal("durable terminal replay emitted lease loss")
	default:
	}
}
