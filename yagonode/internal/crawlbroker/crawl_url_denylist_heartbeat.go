package crawlbroker

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (s *exchangeServer) urlDenylistForHeartbeat(
	req *crawlrpc.WorkerHeartbeat,
) (*crawlrpc.CrawlURLDenylist, error) {
	revision := req.GetUrlDenylistRevision()
	if !yagocrawlcontract.ValidCrawlURLDenylistRevision(revision) {
		return nil, status.Error(codes.InvalidArgument, "invalid crawl URL denylist revision")
	}
	if !req.GetUrlDenylistBootstrap() && len(revision) == 0 {
		return nil, nil
	}
	policy, err := s.urlDenylist.Snapshot(revision)
	if err == nil {
		return policy, nil
	}
	if errors.Is(err, errCrawlURLDenylistUnavailable) {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	return nil, status.Error(codes.FailedPrecondition, err.Error())
}

func validURLDenylistBootstrapHeartbeat(req *crawlrpc.WorkerHeartbeat) bool {
	return len(req.GetActiveLeaseIds()) == 0 &&
		len(req.GetAcknowledgedDirectiveIds()) == 0 &&
		req.ActiveFetches == nil &&
		req.ConfirmActiveLeaseDeliveries == nil &&
		req.StorageAvailableBytes == nil &&
		req.StoragePressure == nil &&
		req.StorageMeasurementAvailable == nil
}
