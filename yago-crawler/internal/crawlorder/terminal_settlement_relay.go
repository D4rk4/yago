package crawlorder

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

const (
	terminalSettlementReconciliationWait        = time.Second
	terminalSettlementReconciliationConcurrency = 4
	msgTerminalSettlementReconcileFailed        = "terminal crawl settlement reconciliation failed"
)

type terminalSettlementRelay struct {
	client   OrderStreamer
	outbox   crawlsettlement.Outbox
	policy   leaseSettlementPolicy
	session  terminalSettlementSession
	signal   chan struct{}
	mutex    sync.Mutex
	inFlight map[string]*terminalSettlementAdvance
}

type terminalSettlementAdvance struct {
	done chan struct{}
	err  error
}

type terminalSettlementDelivery func(
	context.Context,
	crawlsettlement.Settlement,
	[]byte,
) (*crawlrpc.OrderAckResult, error)

func newTerminalSettlementRelay(
	client OrderStreamer,
	outbox crawlsettlement.Outbox,
) *terminalSettlementRelay {
	return &terminalSettlementRelay{
		client: client,
		outbox: outbox,
		policy: leaseSettlementPolicy{
			retryWait:        DefaultLeaseSettlementRetryWait,
			maximumRetryWait: maximumLeaseSettlementRetryWait,
			shutdownWait:     DefaultLeaseSettlementShutdownWait,
		},
		signal:   make(chan struct{}, 1),
		inFlight: make(map[string]*terminalSettlementAdvance),
	}
}

func (relay *terminalSettlementRelay) stageAndDeliver(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
) error {
	localCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		relay.policy.shutdownWait,
	)
	defer cancel()
	settlement = crawlsettlement.Clone(settlement)
	if !crawlsettlement.Validate(settlement) {
		return fmt.Errorf("stage terminal crawl settlement: invalid definition")
	}
	if err := relay.outbox.Stage(localCtx, settlement); err != nil {
		return fmt.Errorf("stage terminal crawl settlement: %w", err)
	}
	relay.wake()

	return relay.advance(localCtx, settlement)
}

func (relay *terminalSettlementRelay) reconcile(ctx context.Context) error {
	awaiting, err := relay.outbox.Awaiting(ctx)
	if err != nil {
		return fmt.Errorf("read terminal crawl settlements: %w", err)
	}
	workers := min(terminalSettlementReconciliationConcurrency, len(awaiting))
	jobs := make(chan crawlsettlement.Settlement)
	results := make(chan error, len(awaiting))
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for settlement := range jobs {
				if err := relay.advanceWithDelivery(
					ctx,
					settlement,
					relay.deliverOnce,
				); err != nil {
					results <- fmt.Errorf(
						"reconcile terminal crawl settlement %q: %w",
						settlement.LeaseID,
						err,
					)
				}
			}
		}()
	}
sendSettlements:
	for _, settlement := range awaiting {
		select {
		case jobs <- settlement:
		case <-ctx.Done():
			break sendSettlements
		}
	}
	close(jobs)
	wait.Wait()
	close(results)
	var reconciliationError error
	for result := range results {
		reconciliationError = errors.Join(reconciliationError, result)
	}
	if ctx.Err() != nil {
		reconciliationError = errors.Join(reconciliationError, ctx.Err())
	}

	return reconciliationError
}

func (relay *terminalSettlementRelay) advance(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
) error {
	return relay.advanceWithDelivery(ctx, settlement, relay.deliver)
}

func (relay *terminalSettlementRelay) advanceWithDelivery(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
	deliver terminalSettlementDelivery,
) error {
	relay.mutex.Lock()
	if current := relay.inFlight[settlement.LeaseID]; current != nil {
		relay.mutex.Unlock()
		select {
		case <-current.done:
			return current.err
		case <-ctx.Done():
			return fmt.Errorf("advance terminal crawl settlement: %w", ctx.Err())
		}
	}
	current := &terminalSettlementAdvance{done: make(chan struct{})}
	relay.inFlight[settlement.LeaseID] = current
	relay.mutex.Unlock()

	current.err = relay.advanceExclusive(ctx, settlement, deliver)
	relay.mutex.Lock()
	delete(relay.inFlight, settlement.LeaseID)
	close(current.done)
	relay.mutex.Unlock()

	return current.err
}

func (relay *terminalSettlementRelay) advanceExclusive(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
	deliver terminalSettlementDelivery,
) error {
	settlement, found, err := relay.currentTerminalSettlement(ctx, settlement)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	settlement, err = relay.acknowledgeTerminalSettlement(ctx, settlement, deliver)
	if err != nil {
		return err
	}

	return relay.confirmTerminalSettlement(ctx, settlement, deliver)
}

func (relay *terminalSettlementRelay) currentTerminalSettlement(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
) (crawlsettlement.Settlement, bool, error) {
	settlement, found, err := relay.rebindAwaitingWorkerSession(ctx, settlement)
	if err != nil {
		return crawlsettlement.Settlement{}, false, fmt.Errorf(
			"rebind terminal crawl settlement worker session: %w",
			err,
		)
	}
	if !found {
		return crawlsettlement.Settlement{}, false, nil
	}
	if !crawlsettlement.Validate(settlement) {
		return crawlsettlement.Settlement{}, false, crawlsettlement.ErrDefinitionConflict
	}
	current, found, err := relay.outbox.Current(
		ctx,
		settlement.LeaseID,
		settlement.OrderIdentity,
	)
	if err != nil {
		return crawlsettlement.Settlement{}, false, fmt.Errorf(
			"read terminal crawl settlement: %w",
			err,
		)
	}
	if !found {
		return crawlsettlement.Settlement{}, false, nil
	}
	if !crawlsettlement.SameDefinition(current, settlement) {
		return crawlsettlement.Settlement{}, false, crawlsettlement.ErrDefinitionConflict
	}

	return current, true, nil
}

func (relay *terminalSettlementRelay) acknowledgeTerminalSettlement(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
	deliver terminalSettlementDelivery,
) (crawlsettlement.Settlement, error) {
	if settlement.Phase == 0 {
		settlement.Phase = crawlsettlement.AwaitingAcknowledgment
	}
	if settlement.Phase != crawlsettlement.AwaitingAcknowledgment {
		return settlement, nil
	}
	result, err := deliver(ctx, settlement, nil)
	if err != nil {
		return crawlsettlement.Settlement{}, err
	}
	token := result.GetConfirmationToken()
	if len(token) != sha256.Size {
		return crawlsettlement.Settlement{}, fmt.Errorf(
			"acknowledge terminal crawl settlement: invalid confirmation token",
		)
	}
	if err := relay.outbox.RecordAcknowledgment(
		ctx,
		settlement.LeaseID,
		settlement.OrderIdentity,
		token,
	); err != nil {
		return crawlsettlement.Settlement{}, fmt.Errorf(
			"record terminal crawl acknowledgment: %w",
			err,
		)
	}
	settlement.Phase = crawlsettlement.AcknowledgedDeleting
	settlement.ConfirmationToken = append([]byte(nil), token...)

	return settlement, nil
}

func (relay *terminalSettlementRelay) confirmTerminalSettlement(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
	deliver terminalSettlementDelivery,
) error {
	if settlement.Phase == crawlsettlement.AcknowledgedDeleting {
		if err := relay.outbox.PrepareConfirmation(
			ctx,
			settlement.LeaseID,
			settlement.OrderIdentity,
		); err != nil {
			return fmt.Errorf("prepare terminal crawl confirmation: %w", err)
		}
		settlement.Phase = crawlsettlement.Confirming
	}
	if _, err := deliver(
		ctx,
		settlement,
		settlement.ConfirmationToken,
	); err != nil {
		return err
	}
	if err := relay.outbox.Complete(
		ctx,
		settlement.LeaseID,
		settlement.OrderIdentity,
	); err != nil {
		return fmt.Errorf("complete terminal crawl settlement: %w", err)
	}

	return nil
}

func (relay *terminalSettlementRelay) deliverOnce(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
	confirmationToken []byte,
) (*crawlrpc.OrderAckResult, error) {
	request := terminalSettlementAcknowledgment(settlement)
	request.ConfirmationToken = append([]byte(nil), confirmationToken...)
	callCtx, cancel := context.WithTimeout(ctx, orderAckTimeout)
	result, err := relay.client.AckOrder(callCtx, request)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("settle crawl order lease: %w", err)
	}

	return result, nil
}

func (relay *terminalSettlementRelay) deliver(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
	confirmationToken []byte,
) (*crawlrpc.OrderAckResult, error) {
	request := terminalSettlementAcknowledgment(settlement)
	request.ConfirmationToken = append([]byte(nil), confirmationToken...)

	return (leaseSettlementSession{
		serviceCtx: ctx,
		client:     relay.client,
		request:    request,
		policy:     relay.policy,
	}).retryResult(ctx)
}

func terminalSettlementAcknowledgment(
	settlement crawlsettlement.Settlement,
) *crawlrpc.OrderAck {
	acknowledgment := &crawlrpc.OrderAck{
		LeaseId:         settlement.LeaseID,
		Requeue:         settlement.Outcome == crawlsettlement.Requeue,
		OrderIdentity:   append([]byte(nil), settlement.OrderIdentity...),
		WorkerId:        settlement.WorkerID,
		WorkerSessionId: settlement.WorkerSessionID,
		TerminalState:   protoRunState(settlement.State),
		TerminalTally:   protoRunTally(settlement.Tally),
	}
	if settlement.RateKnown {
		rate := settlement.PagesPerMinute
		acknowledgment.PagesPerMinute = &rate
	}

	return acknowledgment
}

func (relay *terminalSettlementRelay) run(ctx context.Context) {
	for {
		if err := relay.reconcile(ctx); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, msgTerminalSettlementReconcileFailed, slog.Any("error", err))
		}
		timer := time.NewTimer(terminalSettlementReconciliationWait)
		select {
		case <-timer.C:
		case <-relay.signal:
			timer.Stop()
		case <-ctx.Done():
			timer.Stop()

			return
		}
	}
}

func (relay *terminalSettlementRelay) wake() {
	select {
	case relay.signal <- struct{}{}:
	default:
	}
}
