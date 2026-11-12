package crawlbroker

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (s *exchangeServer) AckOrder(
	ctx context.Context,
	req *crawlrpc.OrderAck,
) (*crawlrpc.OrderAckResult, error) {
	leaseID := req.GetLeaseId()
	if leaseID == "" {
		return nil, status.Error(codes.InvalidArgument, "empty lease id")
	}
	workerID := req.GetWorkerId()
	workerSessionID := req.GetWorkerSessionId()
	if !validCrawlerLeaseIdentity(workerID, workerSessionID) {
		return nil, status.Error(codes.InvalidArgument, "invalid worker session identity")
	}
	retryAfter := time.Duration(req.GetRetryAfterNanoseconds())
	if retryAfter < 0 || retryAfter > yagocrawlcontract.MaximumCrawlBackoffDelay ||
		retryAfter > 0 && !req.GetRequeue() {
		return nil, status.Error(codes.InvalidArgument, "invalid crawl order retry delay")
	}
	terminal, rich, err := terminalLeaseRequestFromProto(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if rich {
		if retryAfter > 0 {
			return nil, status.Error(codes.InvalidArgument, "retry delay requires a requeue")
		}
		confirmationToken, err := s.settleTerminalOrder(ctx, leaseID, terminal)
		if err != nil {
			return nil, terminalSettlementRefusalStatus(err)
		}
		s.sessions.confirmDisposition(workerID, workerSessionID, leaseID)

		return &crawlrpc.OrderAckResult{ConfirmationToken: confirmationToken}, nil
	}
	settle := func(settlementContext context.Context, settledLeaseID string) error {
		return s.acknowledgeOrderForOwner(
			settlementContext,
			settledLeaseID,
			workerID,
			workerSessionID,
		)
	}
	if req.GetRequeue() {
		settle = func(settlementContext context.Context, settledLeaseID string) error {
			if retryAfter > 0 {
				return s.queue.deferLeaseForOwnerAfter(
					settlementContext,
					settledLeaseID,
					workerID,
					workerSessionID,
					retryAfter,
				)
			}
			return s.queue.deferLeaseForOwner(
				settlementContext,
				settledLeaseID,
				workerID,
				workerSessionID,
			)
		}
	}
	if err := settle(ctx, leaseID); err != nil {
		if errors.Is(err, errLeaseDispositionConflict) || errors.Is(err, errLeaseLost) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	s.sessions.confirmDisposition(workerID, workerSessionID, leaseID)

	return &crawlrpc.OrderAckResult{}, nil
}
