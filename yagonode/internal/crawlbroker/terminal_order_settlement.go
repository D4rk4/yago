package crawlbroker

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type terminalLeaseRequest struct {
	Outcome           leaseSettlementOutcome
	OrderIdentity     []byte
	WorkerID          string
	WorkerSessionID   string
	State             yagocrawlcontract.CrawlRunState
	Tally             yagocrawlcontract.CrawlRunTally
	Rate              uint32
	RateKnown         bool
	ConfirmationToken []byte
}

func terminalLeaseRequestFromProto(
	acknowledgment *crawlrpc.OrderAck,
) (terminalLeaseRequest, bool, error) {
	rich := len(acknowledgment.GetOrderIdentity()) != 0 ||
		acknowledgment.GetTerminalState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_UNSPECIFIED ||
		acknowledgment.GetTerminalTally() != nil || acknowledgment.PagesPerMinute != nil ||
		len(acknowledgment.GetConfirmationToken()) != 0
	if !rich {
		return terminalLeaseRequest{}, false, nil
	}
	state, err := terminalRunStateFromProto(acknowledgment.GetTerminalState())
	if err != nil {
		return terminalLeaseRequest{}, true, err
	}
	request := terminalLeaseRequest{
		Outcome:           leaseSettlementAcknowledged,
		OrderIdentity:     append([]byte(nil), acknowledgment.GetOrderIdentity()...),
		WorkerID:          acknowledgment.GetWorkerId(),
		WorkerSessionID:   acknowledgment.GetWorkerSessionId(),
		State:             state,
		Tally:             tallyFromProto(acknowledgment.GetTerminalTally()),
		Rate:              acknowledgment.GetPagesPerMinute(),
		RateKnown:         acknowledgment.PagesPerMinute != nil,
		ConfirmationToken: append([]byte(nil), acknowledgment.GetConfirmationToken()...),
	}
	if acknowledgment.GetRequeue() {
		request.Outcome = leaseSettlementRequeued
	}
	if !yagocrawlcontract.ValidCrawlLeaseID(acknowledgment.GetLeaseId()) ||
		len(request.OrderIdentity) != sha256.Size ||
		!validCrawlerLeaseIdentity(request.WorkerID, request.WorkerSessionID) ||
		acknowledgment.GetTerminalTally() == nil || request.Tally.Pending != 0 ||
		len(request.ConfirmationToken) != 0 && len(request.ConfirmationToken) != sha256.Size {
		return terminalLeaseRequest{}, true, fmt.Errorf("invalid terminal crawl lease definition")
	}

	return request, true, nil
}

func terminalRunStateFromProto(
	state crawlrpc.CrawlRunState,
) (yagocrawlcontract.CrawlRunState, error) {
	switch state {
	case crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED:
		return yagocrawlcontract.CrawlRunFinished, nil
	case crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED:
		return yagocrawlcontract.CrawlRunCancelled, nil
	default:
		return "", fmt.Errorf("invalid terminal crawl run state")
	}
}

func validateTerminalLeaseDefinition(
	orderIdentity []byte,
	progress yagocrawlcontract.CrawlRunProgress,
) error {
	if len(orderIdentity) != sha256.Size || progress.WorkerID == "" || progress.RunID == "" ||
		progress.Tally.Pending != 0 ||
		progress.State != yagocrawlcontract.CrawlRunFinished &&
			progress.State != yagocrawlcontract.CrawlRunCancelled {
		return fmt.Errorf("invalid terminal crawl lease definition")
	}

	return nil
}

func (q *DurableOrderQueue) prepareTerminalLeaseSettlement(
	ctx context.Context,
	leaseID string,
	request terminalLeaseRequest,
) (leaseSettlementRecord, error) {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	want := terminalSettlementRecord(request)
	var settlement leaseSettlementRecord
	var removed leaseRecord
	removedFound := false
	err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		settlement, removed, removedFound, err = q.prepareTerminalLeaseSettlementTx(
			tx,
			leaseID,
			request,
			want,
		)

		return err
	})
	if err != nil {
		return leaseSettlementRecord{}, fmt.Errorf("settle terminal crawl lease: %w", err)
	}
	if removedFound {
		q.workerLeases.remove(removed)
	}
	q.signal()

	return settlement, nil
}

func (q *DurableOrderQueue) acknowledgeTerminalProgress(
	ctx context.Context,
	leaseID string,
	definition leaseSettlementRecord,
) error {
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		record, err := q.requireTerminalLeaseSettlement(tx, leaseID, definition)
		if err != nil || record.ProgressDelivered {
			return err
		}
		record.ProgressDelivered = true

		return q.leaseSettlements.Put(tx, vault.Key(leaseID), record)
	}); err != nil {
		return fmt.Errorf("acknowledge terminal crawl progress: %w", err)
	}

	return nil
}

func (s *exchangeServer) settleTerminalOrder(
	ctx context.Context,
	leaseID string,
	request terminalLeaseRequest,
) ([]byte, error) {
	if len(request.ConfirmationToken) != 0 {
		if !validTerminalSettlementToken(s.queue.terminalSettlementSecret, leaseID, request) {
			return nil, errLeaseDispositionConflict
		}
		if err := s.queue.confirmTerminalLeaseSettlement(ctx, leaseID, request); err != nil {
			return nil, err
		}

		return nil, nil
	}
	settlement, err := s.queue.prepareTerminalLeaseSettlement(ctx, leaseID, request)
	if err != nil {
		return nil, err
	}
	if !settlement.ProgressDelivered {
		if err := s.progress.RecordTerminal(
			ctx,
			settlement.OrderIdentity,
			settlement.Progress,
		); err != nil {
			return nil, fmt.Errorf("deliver terminal crawl progress: %w", err)
		}
		if err := s.queue.acknowledgeTerminalProgress(ctx, leaseID, settlement); err != nil {
			return nil, err
		}
	}
	if err := s.progress.ConfirmTerminalDelivery(ctx, settlement.OrderIdentity); err != nil {
		return nil, fmt.Errorf("confirm terminal crawl progress delivery: %w", err)
	}
	if settlement.Outcome == leaseSettlementAcknowledged {
		if err := s.acknowledgeOrder(ctx, leaseID); err != nil {
			return nil, err
		}
	}
	if err := s.queue.finalizeTerminalLeaseSettlement(ctx, leaseID, settlement); err != nil {
		return nil, err
	}

	return terminalSettlementToken(s.queue.terminalSettlementSecret, leaseID, request), nil
}
