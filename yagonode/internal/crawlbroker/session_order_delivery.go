package crawlbroker

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (s *exchangeServer) streamRecoveredOrders(
	stream crawlrpc.CrawlExchange_StreamOrdersServer,
	workerID string,
	workerSessionID string,
	generation uint64,
	orders []leasedCrawlOrder,
) error {
	if len(orders) > yagocrawlcontract.MaximumHeartbeatActiveLeases {
		return status.Error(codes.ResourceExhausted, "recovered crawl lease capacity exceeded")
	}
	recoveredLeaseIDs := make([]string, len(orders))
	for index, order := range orders {
		recoveredLeaseIDs[index] = order.LeaseID
	}
	for index, order := range orders {
		if err := s.sessions.whileCurrentRegistration(
			workerID,
			workerSessionID,
			generation,
			func() error { return nil },
		); err != nil {
			return status.Error(codes.FailedPrecondition, err.Error())
		}
		message := &crawlrpc.CrawlOrderMessage{
			OrderJson:         order.OrderData,
			LeaseId:           order.LeaseID,
			Recovered:         true,
			RecoveredBatchEnd: index == len(orders)-1,
		}
		if index == 0 {
			message.RecoveredLeaseIds = recoveredLeaseIDs
		}
		if err := stream.Send(message); err != nil {
			return fmt.Errorf("send recovered crawl order: %w", err)
		}
	}

	return nil
}

func (s *exchangeServer) streamNewOrders(
	ctx context.Context,
	stream crawlrpc.CrawlExchange_StreamOrdersServer,
	workerID string,
	workerSessionID string,
	generation uint64,
) error {
	for {
		data, leaseID, err := s.leaseNextForSession(
			ctx,
			workerID,
			workerSessionID,
			generation,
		)
		if err != nil {
			return streamLeaseError(ctx, stream.Context(), err)
		}
		if err := stream.Send(
			&crawlrpc.CrawlOrderMessage{OrderJson: data, LeaseId: leaseID},
		); err != nil {
			return fmt.Errorf("send crawl order: %w", err)
		}
	}
}

func streamLeaseError(
	sessionContext context.Context,
	streamContext context.Context,
	err error,
) error {
	if errors.Is(err, errLeaseLost) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	if streamContext.Err() != nil {
		return status.FromContextError(sessionContext.Err()).Err()
	}
	if sessionContext.Err() != nil {
		return status.Error(codes.FailedPrecondition, errLeaseLost.Error())
	}

	return status.Errorf(codes.Internal, "lease crawl order: %v", err)
}
