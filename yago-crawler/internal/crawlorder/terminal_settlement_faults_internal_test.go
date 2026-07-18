package crawlorder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type terminalSettlementOutboxScenario struct {
	mutex          sync.Mutex
	settlement     crawlsettlement.Settlement
	stage          func(context.Context, crawlsettlement.Settlement) error
	awaiting       func(context.Context) ([]crawlsettlement.Settlement, error)
	current        func(context.Context, string, []byte) (crawlsettlement.Settlement, bool, error)
	rebind         func(context.Context, crawlsettlement.Settlement, string) (crawlsettlement.Settlement, bool, error)
	recordError    error
	prepareError   error
	completeError  error
	completeCalled bool
}

func (scenario *terminalSettlementOutboxScenario) Stage(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
) error {
	if scenario.stage != nil {
		return scenario.stage(ctx, settlement)
	}
	scenario.mutex.Lock()
	scenario.settlement = crawlsettlement.Clone(settlement)
	scenario.mutex.Unlock()

	return nil
}

func (scenario *terminalSettlementOutboxScenario) Awaiting(
	ctx context.Context,
) ([]crawlsettlement.Settlement, error) {
	if scenario.awaiting != nil {
		return scenario.awaiting(ctx)
	}
	scenario.mutex.Lock()
	defer scenario.mutex.Unlock()
	if scenario.settlement.LeaseID == "" {
		return nil, nil
	}

	return []crawlsettlement.Settlement{crawlsettlement.Clone(scenario.settlement)}, nil
}

func (scenario *terminalSettlementOutboxScenario) Current(
	ctx context.Context,
	leaseID string,
	orderIdentity []byte,
) (crawlsettlement.Settlement, bool, error) {
	if scenario.current != nil {
		return scenario.current(ctx, leaseID, orderIdentity)
	}
	scenario.mutex.Lock()
	defer scenario.mutex.Unlock()
	if scenario.settlement.LeaseID != leaseID ||
		!bytes.Equal(scenario.settlement.OrderIdentity, orderIdentity) {
		return crawlsettlement.Settlement{}, false, nil
	}

	return crawlsettlement.Clone(scenario.settlement), true, nil
}

func (scenario *terminalSettlementOutboxScenario) RecordAcknowledgment(
	context.Context,
	string,
	[]byte,
	[]byte,
) error {
	return scenario.recordError
}

func (scenario *terminalSettlementOutboxScenario) PrepareConfirmation(
	context.Context,
	string,
	[]byte,
) error {
	return scenario.prepareError
}

func (scenario *terminalSettlementOutboxScenario) Complete(
	context.Context,
	string,
	[]byte,
) error {
	scenario.mutex.Lock()
	scenario.completeCalled = true
	scenario.mutex.Unlock()

	return scenario.completeError
}

func (scenario *terminalSettlementOutboxScenario) RebindWorkerSession(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
	workerSessionID string,
) (crawlsettlement.Settlement, bool, error) {
	if scenario.rebind != nil {
		return scenario.rebind(ctx, settlement, workerSessionID)
	}

	return settlement, true, nil
}

func terminalSettlementScenarioDefinition() crawlsettlement.Settlement {
	return crawlsettlement.Settlement{
		LeaseID:         "scenario-lease",
		OrderIdentity:   bytes.Repeat([]byte{7}, sha256.Size),
		Provenance:      []byte("scenario-run"),
		WorkerID:        "scenario-worker",
		WorkerSessionID: "scenario-session",
		Outcome:         crawlsettlement.Delete,
		State:           yagocrawlcontract.CrawlRunFinished,
		Tally:           yagocrawlcontract.CrawlRunTally{Fetched: 1},
	}
}

func TestTerminalSettlementRejectsInvalidDefinitionBeforeDurableStage(t *testing.T) {
	staged := false
	outbox := &terminalSettlementOutboxScenario{stage: func(
		context.Context,
		crawlsettlement.Settlement,
	) error {
		staged = true

		return nil
	}}
	relay := newTerminalSettlementRelay(&terminalAcknowledgmentClient{}, outbox)
	invalid := terminalSettlementScenarioDefinition()
	invalid.LeaseID = ""
	if err := relay.stageAndDeliver(t.Context(), invalid); err == nil {
		t.Fatal("invalid terminal settlement was accepted")
	}
	if staged {
		t.Fatal("invalid terminal settlement reached durable stage")
	}
}

func TestGRPCOrderReceiverBindsTerminalSettlementOutbox(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	client := &fakeStreamer{ctx: ctx}
	outbox := &terminalSettlementOutboxScenario{}
	receiver := NewGRPCOrderReceiver(
		ctx,
		client,
		"terminal-worker",
		nil,
		WithTerminalSettlementOutbox(outbox),
	)
	cancel()
	drainUntilClosed(t, receiver)
}

func TestDeliveredOrderStagesRichTerminalRequeue(t *testing.T) {
	token := bytes.Repeat([]byte{5}, sha256.Size)
	client := &terminalAcknowledgmentClient{token: token}
	outbox := &terminalSettlementOutboxScenario{}
	relay := newTerminalSettlementRelay(client, outbox)
	out := make(chan CrawlOrderDelivery, 1)
	identity := bytes.Repeat([]byte{3}, sha256.Size)
	order := yagocrawlcontract.CrawlOrder{Provenance: []byte("rich-terminal-run")}
	if !deliverOrderWithLeaseSession(t.Context(), crawlOrderDeliveryEnvelope{
		client:              client,
		out:                 out,
		order:               order,
		orderIdentity:       identity,
		leaseID:             "rich-terminal-lease",
		workerID:            "rich-terminal-worker",
		terminalSettlements: relay,
		heartbeat: &heartbeatDelivery{
			workerSessionID: "rich-terminal-session",
		},
	}) {
		t.Fatal("terminal-capable order was not delivered")
	}
	delivery := <-out
	if delivery.settleTerminal == nil {
		t.Fatal("terminal-capable delivery omitted durable settlement")
	}
	if err := delivery.settleTerminal(t.Context(), terminalRunSettlement{
		Disposition:    crawlOrderRequeued,
		State:          yagocrawlcontract.CrawlRunCancelled,
		Tally:          yagocrawlcontract.CrawlRunTally{Fetched: 2},
		PagesPerMinute: 37,
		RateKnown:      true,
	}); err != nil {
		t.Fatalf("stage rich terminal requeue: %v", err)
	}
	calls := client.acknowledgments()
	if len(calls) != 2 || !calls[0].GetRequeue() || calls[0].GetPagesPerMinute() != 37 ||
		calls[0].GetWorkerSessionId() != "rich-terminal-session" ||
		!bytes.Equal(calls[1].GetConfirmationToken(), token) {
		t.Fatalf("rich terminal acknowledgments = %+v", calls)
	}
}

func TestTerminalSettlementDurableLookupFailsClosed(t *testing.T) {
	definition := terminalSettlementScenarioDefinition()
	fault := errors.New("durable lookup failed")
	tests := []struct {
		name    string
		input   crawlsettlement.Settlement
		current func(context.Context, string, []byte) (crawlsettlement.Settlement, bool, error)
		wantErr bool
	}{
		{
			name:  "invalid staged definition",
			input: crawlsettlement.Settlement{},
			current: func(context.Context, string, []byte) (
				crawlsettlement.Settlement,
				bool,
				error,
			) {
				t.Fatal("invalid definition reached durable lookup")

				return crawlsettlement.Settlement{}, false, nil
			},
			wantErr: true,
		},
		{
			name:  "lookup failure",
			input: definition,
			current: func(context.Context, string, []byte) (
				crawlsettlement.Settlement,
				bool,
				error,
			) {
				return crawlsettlement.Settlement{}, false, fault
			},
			wantErr: true,
		},
		{
			name:  "missing durable row",
			input: definition,
			current: func(context.Context, string, []byte) (
				crawlsettlement.Settlement,
				bool,
				error,
			) {
				return crawlsettlement.Settlement{}, false, nil
			},
		},
		{
			name:  "conflicting durable row",
			input: definition,
			current: func(context.Context, string, []byte) (
				crawlsettlement.Settlement,
				bool,
				error,
			) {
				conflict := crawlsettlement.Clone(definition)
				conflict.WorkerID = "other-worker"

				return conflict, true, nil
			},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outbox := &terminalSettlementOutboxScenario{current: test.current}
			relay := newTerminalSettlementRelay(&terminalAcknowledgmentClient{}, outbox)
			err := relay.advance(t.Context(), test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("terminal lookup error = %v, want error %t", err, test.wantErr)
			}
		})
	}
}

func TestTerminalSettlementSessionRebindingHandlesDisappearanceAndFailure(t *testing.T) {
	definition := terminalSettlementScenarioDefinition()
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	if err := registry.Track(definition.LeaseID); err != nil {
		t.Fatal(err)
	}
	registry.Renew(
		time.Now(),
		time.Hour,
		[]string{definition.LeaseID},
		[]string{definition.LeaseID},
	)
	definition.Phase = crawlsettlement.AwaitingAcknowledgment
	definition.WorkerSessionID = "previous-session"
	fault := errors.New("rebind write failed")
	for _, test := range []struct {
		name    string
		rebind  func(context.Context, crawlsettlement.Settlement, string) (crawlsettlement.Settlement, bool, error)
		wantErr bool
	}{
		{
			name: "row disappeared",
			rebind: func(
				context.Context,
				crawlsettlement.Settlement,
				string,
			) (crawlsettlement.Settlement, bool, error) {
				return crawlsettlement.Settlement{}, false, nil
			},
		},
		{
			name: "durable write failed",
			rebind: func(
				context.Context,
				crawlsettlement.Settlement,
				string,
			) (crawlsettlement.Settlement, bool, error) {
				return crawlsettlement.Settlement{}, false, fault
			},
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			outbox := &terminalSettlementOutboxScenario{rebind: test.rebind}
			relay := newTerminalSettlementRelay(&terminalAcknowledgmentClient{}, outbox)
			relay.bindWorkerLeaseSession(definition.WorkerID, "replacement-session", registry)
			err := relay.advance(t.Context(), definition)
			if (err != nil) != test.wantErr {
				t.Fatalf("terminal rebind error = %v, want error %t", err, test.wantErr)
			}
		})
	}
}

func TestTerminalSettlementResumesNewDelivery(t *testing.T) {
	token := bytes.Repeat([]byte{8}, sha256.Size)
	assertTerminalSettlementPhaseResumes(t, 0, token, 2)
}

func TestTerminalSettlementResumesConfirmationDelivery(t *testing.T) {
	token := bytes.Repeat([]byte{8}, sha256.Size)
	assertTerminalSettlementPhaseResumes(t, crawlsettlement.Confirming, token, 1)
}

func assertTerminalSettlementPhaseResumes(
	t *testing.T,
	phase crawlsettlement.Phase,
	token []byte,
	wantCalls int,
) {
	t.Helper()
	current := terminalSettlementScenarioDefinition()
	current.Phase = phase
	if phase == crawlsettlement.Confirming {
		current.ConfirmationToken = append([]byte(nil), token...)
	}
	outbox := &terminalSettlementOutboxScenario{settlement: current}
	relay := newTerminalSettlementRelay(&terminalAcknowledgmentClient{}, outbox)
	calls := 0
	err := relay.advanceWithDelivery(
		t.Context(),
		current,
		func(
			context.Context,
			crawlsettlement.Settlement,
			[]byte,
		) (*crawlrpc.OrderAckResult, error) {
			calls++
			if calls == 1 && phase == 0 {
				return &crawlrpc.OrderAckResult{ConfirmationToken: token}, nil
			}

			return &crawlrpc.OrderAckResult{}, nil
		},
	)
	if err != nil {
		t.Fatalf("resume terminal phase %d: %v", phase, err)
	}
	if calls != wantCalls || !outbox.completeCalled {
		t.Fatalf("phase %d calls/completed = %d/%t", phase, calls, outbox.completeCalled)
	}
}

func TestTerminalSettlementConfirmationSurfacesEveryDurableFault(t *testing.T) {
	definition := terminalSettlementScenarioDefinition()
	definition.Phase = crawlsettlement.AcknowledgedDeleting
	definition.ConfirmationToken = bytes.Repeat([]byte{6}, sha256.Size)
	fault := errors.New("terminal confirmation fault")
	for _, test := range []struct {
		name          string
		prepareError  error
		completeError error
		deliveryError error
	}{
		{name: "prepare", prepareError: fault},
		{name: "delivery", deliveryError: fault},
		{name: "complete", completeError: fault},
	} {
		t.Run(test.name, func(t *testing.T) {
			outbox := &terminalSettlementOutboxScenario{
				settlement:    definition,
				prepareError:  test.prepareError,
				completeError: test.completeError,
			}
			relay := newTerminalSettlementRelay(&terminalAcknowledgmentClient{}, outbox)
			err := relay.advanceWithDelivery(
				t.Context(),
				definition,
				func(
					context.Context,
					crawlsettlement.Settlement,
					[]byte,
				) (*crawlrpc.OrderAckResult, error) {
					return &crawlrpc.OrderAckResult{}, test.deliveryError
				},
			)
			if !errors.Is(err, fault) {
				t.Fatalf("terminal %s fault = %v", test.name, err)
			}
		})
	}
}

func TestConcurrentTerminalAdvanceCanStopWaitingForTheOwner(t *testing.T) {
	definition := terminalSettlementScenarioDefinition()
	definition.Phase = crawlsettlement.AwaitingAcknowledgment
	outbox := &terminalSettlementOutboxScenario{settlement: definition}
	relay := newTerminalSettlementRelay(&terminalAcknowledgmentClient{}, outbox)
	started := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	var firstCall sync.Once
	deliver := func(
		ctx context.Context,
		_ crawlsettlement.Settlement,
		confirmationToken []byte,
	) (*crawlrpc.OrderAckResult, error) {
		if len(confirmationToken) == 0 {
			firstCall.Do(func() { close(started) })
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			return &crawlrpc.OrderAckResult{
				ConfirmationToken: bytes.Repeat([]byte{4}, sha256.Size),
			}, nil
		}

		return &crawlrpc.OrderAckResult{}, nil
	}
	go func() {
		first <- relay.advanceWithDelivery(t.Context(), definition, deliver)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first terminal advance did not acquire ownership")
	}
	waitCtx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := relay.advanceWithDelivery(waitCtx, definition, deliver); err == nil {
		t.Fatal("cancelled terminal waiter remained attached to owner")
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("terminal owner advance: %v", err)
	}
}

func TestTerminalReconciliationCancellationClosesAFullWorkerWindow(t *testing.T) {
	definition := terminalSettlementScenarioDefinition()
	definitions := make([]crawlsettlement.Settlement, 5)
	for index := range definitions {
		definitions[index] = crawlsettlement.Clone(definition)
		definitions[index].LeaseID = "window-lease-" + string(rune('a'+index))
	}
	started := make(chan struct{}, terminalSettlementReconciliationConcurrency)
	outbox := &terminalSettlementOutboxScenario{
		awaiting: func(context.Context) ([]crawlsettlement.Settlement, error) {
			return definitions, nil
		},
		current: func(ctx context.Context, _ string, _ []byte) (
			crawlsettlement.Settlement,
			bool,
			error,
		) {
			started <- struct{}{}
			<-ctx.Done()

			return crawlsettlement.Settlement{}, false, ctx.Err()
		},
	}
	relay := newTerminalSettlementRelay(&terminalAcknowledgmentClient{}, outbox)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- relay.reconcile(ctx) }()
	for range terminalSettlementReconciliationConcurrency {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("terminal reconciliation did not fill its worker window")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled terminal reconciliation = %v", err)
	}
}

type terminalSettlementLogSink struct {
	message chan string
}

func (sink terminalSettlementLogSink) Enabled(context.Context, slog.Level) bool {
	return true
}

func (sink terminalSettlementLogSink) Handle(_ context.Context, record slog.Record) error {
	select {
	case sink.message <- record.Message:
	default:
	}

	return nil
}

func (sink terminalSettlementLogSink) WithAttrs([]slog.Attr) slog.Handler {
	return sink
}

func (sink terminalSettlementLogSink) WithGroup(string) slog.Handler {
	return sink
}

func TestTerminalSettlementRunWakesAfterRecoverableReconciliationFault(t *testing.T) {
	previous := slog.Default()
	messages := make(chan string, 4)
	slog.SetDefault(slog.New(terminalSettlementLogSink{message: messages}))
	t.Cleanup(func() { slog.SetDefault(previous) })
	fault := errors.New("outbox unavailable")
	outbox := &terminalSettlementOutboxScenario{
		awaiting: func(context.Context) ([]crawlsettlement.Settlement, error) {
			return nil, fault
		},
	}
	relay := newTerminalSettlementRelay(&terminalAcknowledgmentClient{}, outbox)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		relay.run(ctx)
		close(done)
	}()
	select {
	case message := <-messages:
		if message != msgTerminalSettlementReconcileFailed {
			t.Fatalf("terminal reconciliation log = %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("recoverable terminal reconciliation fault was not logged")
	}
	relay.wake()
	select {
	case message := <-messages:
		if message != msgTerminalSettlementReconcileFailed {
			t.Fatalf("woken terminal reconciliation log = %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal reconciliation wake did not retry")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("terminal reconciliation run did not close")
	}
}
