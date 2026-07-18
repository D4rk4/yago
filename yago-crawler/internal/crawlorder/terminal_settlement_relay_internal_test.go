package crawlorder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type terminalAcknowledgmentClient struct {
	mutex        sync.Mutex
	calls        []*crawlrpc.OrderAck
	token        []byte
	settled      bool
	confirmed    bool
	afterInitial func()
	initialOnce  sync.Once
	initialStart chan struct{}
	initialReady <-chan struct{}
}

func (client *terminalAcknowledgmentClient) StreamOrders(
	context.Context,
	*crawlrpc.WorkerRegistration,
	...grpc.CallOption,
) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error) {
	return nil, status.Error(codes.Unimplemented, "unused")
}

func (client *terminalAcknowledgmentClient) Heartbeat(
	context.Context,
	*crawlrpc.WorkerHeartbeat,
	...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	return nil, status.Error(codes.Unimplemented, "unused")
}

func (client *terminalAcknowledgmentClient) AckOrder(
	ctx context.Context,
	request *crawlrpc.OrderAck,
	_ ...grpc.CallOption,
) (*crawlrpc.OrderAckResult, error) {
	copy := proto.Clone(request).(*crawlrpc.OrderAck)
	client.mutex.Lock()
	client.calls = append(client.calls, copy)
	confirming := len(request.GetConfirmationToken()) != 0
	if confirming {
		if !client.settled || !bytes.Equal(request.GetConfirmationToken(), client.token) {
			client.mutex.Unlock()

			return nil, status.Error(codes.FailedPrecondition, "invalid confirmation")
		}
		client.confirmed = true
		client.mutex.Unlock()

		return &crawlrpc.OrderAckResult{}, nil
	}
	if client.confirmed {
		client.mutex.Unlock()

		return nil, status.Error(codes.FailedPrecondition, "settlement already confirmed")
	}
	client.settled = true
	client.mutex.Unlock()
	if client.initialStart != nil {
		client.initialOnce.Do(func() { close(client.initialStart) })
	}
	if client.initialReady != nil {
		select {
		case <-client.initialReady:
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		}
	}
	if client.afterInitial != nil {
		client.initialOnce.Do(client.afterInitial)
	}

	return &crawlrpc.OrderAckResult{
		ConfirmationToken: append([]byte(nil), client.token...),
	}, nil
}

func (client *terminalAcknowledgmentClient) acknowledgments() []*crawlrpc.OrderAck {
	client.mutex.Lock()
	defer client.mutex.Unlock()

	return append([]*crawlrpc.OrderAck(nil), client.calls...)
}

func terminalRelayCheckpoint(
	t *testing.T,
	path string,
) (*frontiercheckpoint.FrontierCheckpoint, crawlsettlement.Settlement) {
	t.Helper()
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("open relay checkpoint: %v", err)
	}
	identity := bytes.Repeat([]byte{1}, sha256.Size)
	provenance := []byte("terminal-relay-run")
	if err := checkpoint.Begin(
		t.Context(),
		provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("begin relay run: %v", err)
	}
	if err := checkpoint.FinishSeeding(
		t.Context(),
		provenance,
		yagocrawlcontract.CrawlRunTally{},
	); err != nil {
		t.Fatalf("finish relay run: %v", err)
	}

	return checkpoint, crawlsettlement.Settlement{
		LeaseID:         "terminal-relay-lease",
		OrderIdentity:   identity,
		Provenance:      provenance,
		WorkerID:        "worker",
		WorkerSessionID: "session",
		Outcome:         crawlsettlement.Delete,
		State:           yagocrawlcontract.CrawlRunFinished,
		Tally:           yagocrawlcontract.CrawlRunTally{Fetched: 1},
	}
}

func TestTerminalSettlementReconcilesCrashAfterInitialRPCBeforeLocalWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.db")
	checkpoint, settlement := terminalRelayCheckpoint(t, path)
	client := &terminalAcknowledgmentClient{
		token: bytes.Repeat([]byte{2}, sha256.Size),
	}
	client.afterInitial = func() {
		if err := checkpoint.Close(); err != nil {
			t.Errorf("close checkpoint after initial rpc: %v", err)
		}
	}
	relay := newTerminalSettlementRelay(client, checkpoint)
	err := relay.stageAndDeliver(t.Context(), settlement)
	if !errors.Is(err, frontiercheckpoint.ErrClosed) {
		t.Fatalf("post-rpc crash error = %v, want closed checkpoint", err)
	}

	reopened, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("reopen terminal checkpoint: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	relay = newTerminalSettlementRelay(client, reopened)
	relay.bindWorkerLeaseSession(
		settlement.WorkerID,
		"replacement-session",
		crawllease.NewGrantRegistry(t.Context(), 1),
	)
	if err := relay.reconcile(t.Context()); err != nil {
		t.Fatalf("reconcile terminal crash: %v", err)
	}
	awaiting, err := reopened.Awaiting(t.Context())
	if err != nil || len(awaiting) != 0 {
		t.Fatalf("reconciled terminal outbox = %+v, %v", awaiting, err)
	}
	status, err := reopened.Status(t.Context(), settlement.Provenance, settlement.OrderIdentity)
	if err != nil || status != frontiercheckpoint.RunMissing {
		t.Fatalf("reconciled terminal run = %v, %v", status, err)
	}
	calls := client.acknowledgments()
	if len(calls) != 3 || len(calls[0].GetConfirmationToken()) != 0 ||
		len(calls[1].GetConfirmationToken()) != 0 ||
		!bytes.Equal(calls[2].GetConfirmationToken(), client.token) {
		t.Fatalf("terminal crash acknowledgments = %+v", calls)
	}
	for _, call := range calls {
		if call.GetWorkerSessionId() != settlement.WorkerSessionID {
			t.Fatalf("token-bearing replay session = %q", call.GetWorkerSessionId())
		}
	}
}

func TestTerminalSettlementReconcileRebindsAdoptedLiveLeaseSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.db")
	checkpoint, settlement := terminalRelayCheckpoint(t, path)
	if err := checkpoint.Stage(t.Context(), settlement); err != nil {
		t.Fatalf("stage restart terminal settlement: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close restart terminal checkpoint: %v", err)
	}
	reopened, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("reopen restart terminal checkpoint: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	replacementSessionID := "replacement-session"
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	if err := registry.Track(settlement.LeaseID); err != nil {
		t.Fatalf("track adopted terminal lease: %v", err)
	}
	started := time.Now()
	registry.Renew(
		started,
		time.Hour,
		[]string{settlement.LeaseID},
		[]string{settlement.LeaseID},
	)
	client := &terminalAcknowledgmentClient{
		token: bytes.Repeat([]byte{8}, sha256.Size),
	}
	relay := newTerminalSettlementRelay(client, reopened)
	relay.bindWorkerLeaseSession(settlement.WorkerID, replacementSessionID, registry)
	if err := relay.reconcile(t.Context()); err != nil {
		t.Fatalf("reconcile adopted terminal lease: %v", err)
	}
	calls := client.acknowledgments()
	if len(calls) != 2 {
		t.Fatalf("adopted terminal calls = %d, want 2", len(calls))
	}
	for index, call := range calls {
		if call.GetWorkerSessionId() != replacementSessionID {
			t.Fatalf("adopted terminal call %d session = %q", index, call.GetWorkerSessionId())
		}
	}
	awaiting, err := reopened.Awaiting(t.Context())
	if err != nil || len(awaiting) != 0 {
		t.Fatalf("adopted terminal outbox = %+v, %v", awaiting, err)
	}
}

func TestTerminalSettlementForegroundAndReconcileShareOneAdvance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.db")
	checkpoint, settlement := terminalRelayCheckpoint(t, path)
	defer func() { _ = checkpoint.Close() }()
	if err := checkpoint.Stage(t.Context(), settlement); err != nil {
		t.Fatalf("stage raced terminal settlement: %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	client := &terminalAcknowledgmentClient{
		token:        bytes.Repeat([]byte{3}, sha256.Size),
		initialStart: started,
		initialReady: release,
	}
	relay := newTerminalSettlementRelay(client, checkpoint)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	foreground := make(chan error, 1)
	go func() { foreground <- relay.advance(ctx, settlement) }()
	<-started
	reconciliation := make(chan error, 1)
	go func() { reconciliation <- relay.reconcile(ctx) }()
	close(release)
	if err := <-foreground; err != nil {
		t.Fatalf("foreground terminal advance: %v", err)
	}
	if err := <-reconciliation; err != nil {
		t.Fatalf("reconciled terminal advance: %v", err)
	}
	calls := client.acknowledgments()
	if len(calls) != 2 || len(calls[0].GetConfirmationToken()) != 0 ||
		!bytes.Equal(calls[1].GetConfirmationToken(), client.token) {
		t.Fatalf("raced terminal acknowledgments = %+v", calls)
	}
}

func stagedTerminalRelaySettlement(
	t *testing.T,
	checkpoint *frontiercheckpoint.FrontierCheckpoint,
	leaseID string,
	identityByte byte,
) crawlsettlement.Settlement {
	t.Helper()
	identity := bytes.Repeat([]byte{identityByte}, sha256.Size)
	provenance := []byte("terminal-relay-" + leaseID)
	if err := checkpoint.Begin(
		t.Context(),
		provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("begin staged terminal relay run %q: %v", leaseID, err)
	}
	if err := checkpoint.FinishSeeding(
		t.Context(),
		provenance,
		yagocrawlcontract.CrawlRunTally{},
	); err != nil {
		t.Fatalf("finish staged terminal relay run %q: %v", leaseID, err)
	}
	settlement := crawlsettlement.Settlement{
		LeaseID:         leaseID,
		OrderIdentity:   identity,
		Provenance:      provenance,
		WorkerID:        "worker",
		WorkerSessionID: "session",
		Outcome:         crawlsettlement.Delete,
		State:           yagocrawlcontract.CrawlRunFinished,
		Tally:           yagocrawlcontract.CrawlRunTally{Fetched: 1},
	}
	if err := checkpoint.Stage(t.Context(), settlement); err != nil {
		t.Fatalf("stage terminal relay run %q: %v", leaseID, err)
	}

	return settlement
}

type slowFirstTerminalClient struct {
	terminalAcknowledgmentClient
	slowLeaseID   string
	releaseSlow   <-chan struct{}
	laterDone     chan struct{}
	laterDoneOnce sync.Once
	stateMutex    sync.Mutex
	active        int
	maximumActive int
}

func (client *slowFirstTerminalClient) AckOrder(
	ctx context.Context,
	request *crawlrpc.OrderAck,
	_ ...grpc.CallOption,
) (*crawlrpc.OrderAckResult, error) {
	client.stateMutex.Lock()
	client.active++
	client.maximumActive = max(client.maximumActive, client.active)
	client.stateMutex.Unlock()
	defer func() {
		client.stateMutex.Lock()
		client.active--
		client.stateMutex.Unlock()
	}()
	if request.GetLeaseId() == client.slowLeaseID {
		select {
		case <-client.releaseSlow:
			return nil, status.Error(codes.FailedPrecondition, "rejected")
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		}
	}
	if len(request.GetConfirmationToken()) == 0 {
		return &crawlrpc.OrderAckResult{
			ConfirmationToken: bytes.Repeat([]byte{9}, sha256.Size),
		}, nil
	}
	client.laterDoneOnce.Do(func() { close(client.laterDone) })

	return &crawlrpc.OrderAckResult{}, nil
}

func (client *slowFirstTerminalClient) maximumConcurrency() int {
	client.stateMutex.Lock()
	defer client.stateMutex.Unlock()

	return client.maximumActive
}

func TestTerminalSettlementReconciliationDoesNotLetSlowFirstRowStarveLaterRows(
	t *testing.T,
) {
	checkpoint, err := frontiercheckpoint.Open(filepath.Join(t.TempDir(), "frontier.db"))
	if err != nil {
		t.Fatalf("open multi-settlement relay checkpoint: %v", err)
	}
	defer func() { _ = checkpoint.Close() }()
	slow := stagedTerminalRelaySettlement(t, checkpoint, "lease-a-slow", 5)
	later := stagedTerminalRelaySettlement(t, checkpoint, "lease-b-later", 6)
	releaseSlow := make(chan struct{})
	laterDone := make(chan struct{})
	client := &slowFirstTerminalClient{
		slowLeaseID: slow.LeaseID,
		releaseSlow: releaseSlow,
		laterDone:   laterDone,
	}
	relay := newTerminalSettlementRelay(client, checkpoint)
	result := make(chan error, 1)
	go func() { result <- relay.reconcile(t.Context()) }()
	select {
	case <-laterDone:
	case <-time.After(time.Second):
		t.Fatal("later terminal settlement was starved by the slow first row")
	}
	close(releaseSlow)
	if err := <-result; err == nil || !bytes.Contains([]byte(err.Error()), []byte(slow.LeaseID)) {
		t.Fatalf("joined terminal reconciliation error = %v", err)
	}
	if maximum := client.maximumConcurrency(); maximum < 2 ||
		maximum > terminalSettlementReconciliationConcurrency {
		t.Fatalf("terminal reconciliation concurrency = %d", maximum)
	}
	awaiting, err := checkpoint.Awaiting(t.Context())
	if err != nil || len(awaiting) != 1 || awaiting[0].LeaseID != slow.LeaseID {
		t.Fatalf("terminal outbox after partial reconciliation = %+v, %v", awaiting, err)
	}
	status, err := checkpoint.Status(t.Context(), later.Provenance, later.OrderIdentity)
	if err != nil || status != frontiercheckpoint.RunMissing {
		t.Fatalf("later terminal run status = %v, %v", status, err)
	}
}

type blockedTerminalOutbox struct {
	blockStage    bool
	blockAwaiting bool
}

func (outbox blockedTerminalOutbox) Stage(
	ctx context.Context,
	_ crawlsettlement.Settlement,
) error {
	if outbox.blockStage {
		<-ctx.Done()

		return fmt.Errorf("blocked terminal stage: %w", ctx.Err())
	}

	return nil
}

func (outbox blockedTerminalOutbox) Awaiting(
	ctx context.Context,
) ([]crawlsettlement.Settlement, error) {
	if outbox.blockAwaiting {
		<-ctx.Done()

		return nil, fmt.Errorf("blocked terminal awaiting: %w", ctx.Err())
	}

	return nil, nil
}

func (blockedTerminalOutbox) Current(
	context.Context,
	string,
	[]byte,
) (crawlsettlement.Settlement, bool, error) {
	return crawlsettlement.Settlement{}, false, nil
}

func (blockedTerminalOutbox) RecordAcknowledgment(
	context.Context,
	string,
	[]byte,
	[]byte,
) error {
	return nil
}

func (blockedTerminalOutbox) PrepareConfirmation(context.Context, string, []byte) error {
	return nil
}

func (blockedTerminalOutbox) Complete(context.Context, string, []byte) error {
	return nil
}

func (blockedTerminalOutbox) RebindWorkerSession(
	context.Context,
	crawlsettlement.Settlement,
	string,
) (crawlsettlement.Settlement, bool, error) {
	return crawlsettlement.Settlement{}, false, crawlsettlement.ErrDefinitionConflict
}

func TestTerminalSettlementForegroundWriteStopsAtDetachedDeadline(t *testing.T) {
	client := &terminalAcknowledgmentClient{token: bytes.Repeat([]byte{4}, sha256.Size)}
	relay := newTerminalSettlementRelay(client, blockedTerminalOutbox{blockStage: true})
	relay.policy.shutdownWait = 20 * time.Millisecond
	started := time.Now()
	err := relay.stageAndDeliver(t.Context(), crawlsettlement.Settlement{
		LeaseID:         "blocked",
		OrderIdentity:   bytes.Repeat([]byte{1}, sha256.Size),
		Provenance:      []byte("blocked-run"),
		WorkerID:        "worker",
		WorkerSessionID: "session",
		Outcome:         crawlsettlement.Delete,
		State:           yagocrawlcontract.CrawlRunFinished,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked terminal write error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("blocked terminal write took %v", elapsed)
	}
	if calls := client.acknowledgments(); len(calls) != 0 {
		t.Fatalf("blocked terminal write made %d rpc calls", len(calls))
	}
}

func TestTerminalSettlementReconciliationStopsWithServiceContext(t *testing.T) {
	relay := newTerminalSettlementRelay(
		&terminalAcknowledgmentClient{},
		blockedTerminalOutbox{blockAwaiting: true},
	)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		relay.run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("terminal reconciliation ignored service cancellation")
	}
}

func TestTerminalSettlementInvalidTokenDoesNotRetrySuccessfulRPC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.db")
	checkpoint, settlement := terminalRelayCheckpoint(t, path)
	defer func() { _ = checkpoint.Close() }()
	client := &terminalAcknowledgmentClient{}
	relay := newTerminalSettlementRelay(client, checkpoint)
	err := relay.stageAndDeliver(t.Context(), settlement)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("invalid confirmation token")) {
		t.Fatalf("invalid terminal token error = %v", err)
	}
	if calls := client.acknowledgments(); len(calls) != 1 {
		t.Fatalf("invalid terminal token rpc calls = %d, want 1", len(calls))
	}
}
