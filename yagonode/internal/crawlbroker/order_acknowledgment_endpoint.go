package crawlbroker

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
	terminal, rich, err := terminalLeaseRequestFromProto(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if rich {
		confirmationToken, err := s.settleTerminalOrder(ctx, leaseID, terminal)
		if err != nil {
			if errors.Is(err, errLeaseDispositionConflict) || errors.Is(err, errLeaseLost) {
				return nil, status.Error(codes.FailedPrecondition, err.Error())
			}

			return nil, status.Error(codes.Internal, err.Error())
		}

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

	return &crawlrpc.OrderAckResult{}, nil
}
