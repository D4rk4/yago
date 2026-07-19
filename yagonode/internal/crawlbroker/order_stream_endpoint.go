package crawlbroker

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (s *exchangeServer) StreamOrders(
	reg *crawlrpc.WorkerRegistration,
	stream crawlrpc.CrawlExchange_StreamOrdersServer,
) error {
	ctx, cancelSession := context.WithCancel(stream.Context())
	defer cancelSession()
	workerID := reg.GetWorkerId()
	workerSessionID := reg.GetWorkerSessionId()
	if !validCrawlerLeaseIdentity(workerID, workerSessionID) {
		return status.Error(codes.InvalidArgument, "invalid worker session identity")
	}
	leasedOrders, generation, err := s.activateWorkerSession(
		ctx,
		workerID,
		workerSessionID,
		cancelSession,
		reg.GetFetchStartLeases(),
	)
	if err != nil {
		if errors.Is(err, errWorkerSessionActive) {
			return status.Error(codes.AlreadyExists, err.Error())
		}
		if errors.Is(err, errFleetFetchPolicyInvalid) ||
			errors.Is(err, errFleetFetchCapabilityRequired) {
			return status.Error(codes.FailedPrecondition, err.Error())
		}
		return status.Error(codes.Internal, err.Error())
	}
	s.control.register(workerID)
	defer s.releaseWorkerSession(workerID, workerSessionID, generation)
	if err := s.streamRecoveredOrders(
		stream,
		workerID,
		workerSessionID,
		generation,
		leasedOrders,
	); err != nil {
		return err
	}

	return s.streamNewOrders(ctx, stream, workerID, workerSessionID, generation)
}

func (s *exchangeServer) releaseWorker(workerID string) {
	s.control.unregister(workerID)
}

func (s *exchangeServer) releaseWorkerSession(
	workerID string,
	workerSessionID string,
	generation uint64,
) {
	deactivated := s.sessions.deactivate(workerID, workerSessionID, generation)
	if deactivated && s.fetchStarts != nil {
		s.fetchStarts.DeactivateSession(workerID, workerSessionID)
	}
	s.releaseWorker(workerID)
}
