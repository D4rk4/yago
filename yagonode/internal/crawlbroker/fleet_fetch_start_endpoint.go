package crawlbroker

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (s *exchangeServer) LeaseFetchStarts(
	_ context.Context,
	request *crawlrpc.FetchStartLeaseRequest,
) (*crawlrpc.FetchStartLeaseDecision, error) {
	if s.fetchStarts == nil {
		return nil, status.Error(
			codes.FailedPrecondition,
			"fetch-start lease policy is unavailable",
		)
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, errFleetFetchRequestInvalid.Error())
	}
	if request.CompletedSequence != nil {
		if err := s.fetchStarts.CompleteLease(
			request.GetWorkerId(),
			request.GetWorkerSessionId(),
			request.GetCompletedSequence(),
		); err != nil {
			return nil, fleetFetchStartStatus(err)
		}
	}
	decision, err := s.fetchStarts.Lease(fleetFetchStartLeaseRequest{
		WorkerID:        request.GetWorkerId(),
		WorkerSessionID: request.GetWorkerSessionId(),
		Sequence:        request.GetSequence(),
		MaximumPermits:  request.GetMaximumPermits(),
	})
	if err != nil {
		return nil, fleetFetchStartStatus(err)
	}
	response := &crawlrpc.FetchStartLeaseDecision{
		Granted:          decision.Granted,
		Sequence:         request.GetSequence(),
		PolicyGeneration: decision.Lease.PolicyGeneration,
		Unlimited:        decision.Lease.Unlimited,
	}
	if !decision.Granted {
		response.RetryAfterNanoseconds = max(
			int64(0),
			decision.RetryAt.Sub(decision.ServerObservedAt).Nanoseconds(),
		)

		return response, nil
	}
	if decision.Lease.Unlimited {
		response.Permits = decision.Lease.Permits

		return response, nil
	}
	for permitOrdinal := uint32(0); permitOrdinal < decision.Lease.Permits; permitOrdinal++ {
		window, found := decision.RelativePermitWindow(permitOrdinal)
		if !found {
			continue
		}
		response.Permits = decision.Lease.Permits - permitOrdinal
		response.FirstPermitOpensAfterNanoseconds = window.OpensAfter.Nanoseconds()
		response.FirstPermitClosesAfterNanoseconds = window.ClosesAfter.Nanoseconds()
		response.PermitIntervalNanoseconds = decision.Lease.PermitInterval.Nanoseconds()

		return response, nil
	}

	return response, nil
}

func fleetFetchStartStatus(err error) error {
	switch {
	case errors.Is(err, errFleetFetchRequestInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, errFleetFetchSessionStale),
		errors.Is(err, errFleetFetchLeaseOutstanding),
		errors.Is(err, errFleetFetchSequenceStale),
		errors.Is(err, errFleetFetchLeaseNotFound),
		errors.Is(err, errFleetFetchLeaseSequenceMismatch):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
