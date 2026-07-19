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
	urlDenylist, err := s.urlDenylistForHeartbeat(req)
	if err != nil {
		return nil, err
	}
	if req.GetUrlDenylistBootstrap() {
		if !validURLDenylistBootstrapHeartbeat(req) {
			return nil, status.Error(
				codes.InvalidArgument,
				"invalid crawl URL denylist bootstrap heartbeat",
			)
		}

		return &crawlrpc.WorkerHeartbeatResult{UrlDenylist: urlDenylist}, nil
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
	err = s.sessions.whileCurrent(workerID, workerSessionID, func() error {
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
	s.confirmHeartbeatDeliveries(req, workerID, workerSessionID, renewed)

	return s.heartbeatResult(directives, renewed, leaseTTL, urlDenylist)
}

func (s *exchangeServer) heartbeatResult(
	directives []yagocrawlcontract.CrawlControlDirective,
	renewed []string,
	leaseTTL time.Duration,
	urlDenylist *crawlrpc.CrawlURLDenylist,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	policy := s.control.StoragePressurePolicy()
	runtimePolicy, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(
		s.control.RuntimePolicy(),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, "invalid crawler runtime policy")
	}
	reservedFree := policy.ReservedFreeBytes
	pressureHysteresis := policy.RecoveryHysteresisBytes
	return &crawlrpc.WorkerHeartbeatResult{
		Directives:                     directivesToProto(directives),
		RenewedLeaseIds:                renewed,
		LeaseTtlMilliseconds:           durationMilliseconds(leaseTTL),
		StorageReservedFreeBytes:       &reservedFree,
		StoragePressureHysteresisBytes: &pressureHysteresis,
		UrlDenylist:                    urlDenylist,
		RuntimePolicy:                  runtimePolicy,
	}, nil
}

func (s *exchangeServer) confirmHeartbeatDeliveries(
	req *crawlrpc.WorkerHeartbeat,
	workerID string,
	workerSessionID string,
	renewed []string,
) {
	if req.ConfirmActiveLeaseDeliveries == nil {
		s.sessions.confirmDeliveries(workerID, workerSessionID, renewed)

		return
	}
	if req.GetConfirmActiveLeaseDeliveries() {
		s.sessions.confirmExactDeliveries(workerID, workerSessionID, renewed)
	}
}
