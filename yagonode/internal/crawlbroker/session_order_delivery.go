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

const maximumRecoveredOrderDeliveryBatch = 16

type recoveredOrderDeliverySession struct {
	workerID        string
	workerSessionID string
	generation      uint64
}

type recoveredOrderDeliveryBatch struct {
	orders          []leasedCrawlOrder
	sessionLeaseIDs []string
}

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
	recoveredSessionLeaseIDs := make([]string, len(orders))
	for index, order := range orders {
		recoveredSessionLeaseIDs[index] = order.LeaseID
	}
	deliverySession := recoveredOrderDeliverySession{
		workerID:        workerID,
		workerSessionID: workerSessionID,
		generation:      generation,
	}
	for start := 0; start < len(orders); start += maximumRecoveredOrderDeliveryBatch {
		end := min(start+maximumRecoveredOrderDeliveryBatch, len(orders))
		var sessionLeaseIDs []string
		if start == 0 {
			sessionLeaseIDs = recoveredSessionLeaseIDs
		}
		if err := s.streamRecoveredOrderBatch(
			stream,
			deliverySession,
			recoveredOrderDeliveryBatch{
				orders:          orders[start:end],
				sessionLeaseIDs: sessionLeaseIDs,
			},
		); err != nil {
			return err
		}
	}

	return nil
}

func (s *exchangeServer) streamRecoveredOrderBatch(
	stream crawlrpc.CrawlExchange_StreamOrdersServer,
	deliverySession recoveredOrderDeliverySession,
	deliveryBatch recoveredOrderDeliveryBatch,
) error {
	recoveredLeaseIDs := make([]string, len(deliveryBatch.orders))
	for index, order := range deliveryBatch.orders {
		recoveredLeaseIDs[index] = order.LeaseID
	}
	confirmation, err := s.sessions.expectDeliveryConfirmation(
		deliverySession.workerID,
		deliverySession.workerSessionID,
		deliverySession.generation,
		recoveredLeaseIDs,
	)
	if err != nil {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	for index, order := range deliveryBatch.orders {
		if err := s.sessions.whileCurrentRegistration(
			deliverySession.workerID,
			deliverySession.workerSessionID,
			deliverySession.generation,
			func() error { return nil },
		); err != nil {
			return status.Error(codes.FailedPrecondition, err.Error())
		}
		message := &crawlrpc.CrawlOrderMessage{
			OrderJson:         order.OrderData,
			LeaseId:           order.LeaseID,
			Recovered:         true,
			RecoveredBatchEnd: index == len(deliveryBatch.orders)-1,
		}
		if index == 0 {
			message.RecoveredLeaseIds = recoveredLeaseIDs
			message.RecoveredSessionLeaseIds = deliveryBatch.sessionLeaseIDs
		}
		if err := stream.Send(message); err != nil {
			return fmt.Errorf("send recovered crawl order: %w", err)
		}
		if index == 0 {
			if err := confirmation.wait(stream.Context()); err != nil {
				return streamLeaseError(stream.Context(), stream.Context(), err)
			}
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
		confirmation, err := s.sessions.expectDeliveryConfirmation(
			workerID,
			workerSessionID,
			generation,
			[]string{leaseID},
		)
		if err != nil {
			return streamLeaseError(ctx, stream.Context(), err)
		}
		if err := stream.Send(
			&crawlrpc.CrawlOrderMessage{OrderJson: data, LeaseId: leaseID},
		); err != nil {
			return fmt.Errorf("send crawl order: %w", err)
		}
		if err := confirmation.wait(ctx); err != nil {
			return streamLeaseError(ctx, stream.Context(), err)
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
