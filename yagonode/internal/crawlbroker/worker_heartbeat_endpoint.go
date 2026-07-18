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

func (s *exchangeServer) Heartbeat(
	ctx context.Context,
	req *crawlrpc.WorkerHeartbeat,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	workerID := req.GetWorkerId()
	workerSessionID := req.GetWorkerSessionId()
	if !validCrawlerLeaseIdentity(workerID, workerSessionID) {
		return nil, status.Error(codes.InvalidArgument, "invalid worker session identity")
	}
	activeLeaseIDs, valid := normalizedHeartbeatLeaseIDs(req.GetActiveLeaseIds())
	if !valid {
		return nil, status.Error(codes.InvalidArgument, "invalid active crawl lease identities")
	}
	acknowledgedDirectiveIDs, valid := normalizedHeartbeatDirectiveAcknowledgments(
		req.GetAcknowledgedDirectiveIds(),
	)
	if !valid {
		return nil, status.Error(codes.InvalidArgument, "invalid crawl directive acknowledgments")
	}
	var renewed []string
	var leaseTTL time.Duration
	var directives []yagocrawlcontract.CrawlControlDirective
	err := s.sessions.whileCurrent(workerID, workerSessionID, func() error {
		var err error
		renewed, leaseTTL, err = s.queue.renewLeases(
			ctx,
			workerID,
			workerSessionID,
			activeLeaseIDs,
		)
		if err != nil {
			return err
		}
		s.control.recordActiveFetches(workerID, req.ActiveFetches)
		s.control.recordStoragePressure(workerID, req)
		directives, err = s.control.deliverForHeartbeat(
			ctx,
			workerID,
			acknowledgedDirectiveIDs,
		)

		return err
	})
	if err != nil {
		if errors.Is(err, errLeaseLost) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}

		return nil, status.Error(codes.Internal, err.Error())
	}
	s.sessions.confirmDeliveries(workerID, workerSessionID, renewed)

	policy := s.control.StoragePressurePolicy()
	reservedFree := policy.ReservedFreeBytes
	pressureHysteresis := policy.RecoveryHysteresisBytes
	return &crawlrpc.WorkerHeartbeatResult{
		Directives:                     directivesToProto(directives),
		RenewedLeaseIds:                renewed,
		LeaseTtlMilliseconds:           durationMilliseconds(leaseTTL),
		StorageReservedFreeBytes:       &reservedFree,
		StoragePressureHysteresisBytes: &pressureHysteresis,
	}, nil
}
