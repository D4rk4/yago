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
	if !validHeartbeatFetchActivity(req) {
		return nil, status.Error(codes.InvalidArgument, "invalid worker fetch activity")
	}
	activePages, valid := activeCrawlerPagesFromHeartbeat(req.GetActivePages())
	if !valid {
		return nil, status.Error(codes.InvalidArgument, "invalid active crawler pages")
	}
	waitingPages, valid := waitingCrawlerPagesFromHeartbeat(req.GetWaitingPages())
	if !valid {
		return nil, status.Error(codes.InvalidArgument, "invalid waiting crawler pages")
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
		s.control.recordHeartbeatFetchActivity(
			workerID,
			workerSessionID,
			req.ActiveFetches,
			req.FetchAdmissionWaiting,
		)
		s.control.recordHeartbeatActivePages(workerID, workerSessionID, activePages)
		s.control.recordHeartbeatWaitingPages(workerID, workerSessionID, waitingPages)
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

func activeCrawlerPagesFromHeartbeat(
	pages []*crawlrpc.ActiveCrawlPage,
) ([]CrawlerActivePage, bool) {
	if len(pages) > yagocrawlcontract.MaximumFetchWorkerConcurrency {
		return nil, false
	}
	activePages := make([]CrawlerActivePage, 0, len(pages))
	for _, page := range pages {
		if page == nil || len(page.GetRunId()) > yagocrawlcontract.MaximumCrawlerWaitingPageRunIDBytes ||
			len(page.GetUrl()) > yagocrawlcontract.MaximumCrawlURLBytes {
			return nil, false
		}
		canonicalURL, valid := yagocrawlcontract.CanonicalURL(page.GetUrl())
		if !valid || canonicalURL != page.GetUrl() || !validCrawlerPageStage(page.GetStage()) {
			return nil, false
		}
		activePages = append(activePages, CrawlerActivePage{
			RunID:               page.GetRunId(),
			URL:                 canonicalURL,
			Stage:               page.GetStage(),
			ElapsedMilliseconds: page.GetElapsedMilliseconds(),
		})
	}
	return activePages, true
}

func waitingCrawlerPagesFromHeartbeat(
	pages []*crawlrpc.WaitingCrawlPage,
) ([]CrawlerWaitingPage, bool) {
	if len(pages) > yagocrawlcontract.MaximumCrawlerWaitingPageReports {
		return nil, false
	}
	waitingPages := make([]CrawlerWaitingPage, 0, len(pages))
	perRunPages := make(map[string]int, len(pages))
	for _, page := range pages {
		if page == nil || page.GetRunId() == "" ||
			len(page.GetRunId()) > yagocrawlcontract.MaximumCrawlerWaitingPageRunIDBytes ||
			len(page.GetUrl()) > yagocrawlcontract.MaximumCrawlURLBytes ||
			page.GetAvailableAfterUnixMilliseconds() < 0 {
			return nil, false
		}
		canonicalURL, valid := yagocrawlcontract.CanonicalURL(page.GetUrl())
		if !valid || canonicalURL != page.GetUrl() ||
			!validWaitingCrawlerPageState(page.GetState()) ||
			!validWaitingCrawlerPageReason(page.GetReason()) {
			return nil, false
		}
		perRunPages[page.GetRunId()]++
		if perRunPages[page.GetRunId()] > yagocrawlcontract.MaximumCrawlerWaitingPagesPerRun {
			return nil, false
		}
		waiting := CrawlerWaitingPage{
			RunID:  page.GetRunId(),
			URL:    canonicalURL,
			State:  page.GetState(),
			Reason: page.GetReason(),
		}
		if available := page.GetAvailableAfterUnixMilliseconds(); available > 0 {
			waiting.AvailableAt = time.UnixMilli(available).UTC()
		}
		waitingPages = append(waitingPages, waiting)
	}
	return waitingPages, true
}

func validWaitingCrawlerPageState(state string) bool {
	switch state {
	case "queued", "deferred":
		return true
	default:
		return false
	}
}

func validWaitingCrawlerPageReason(reason string) bool {
	switch reason {
	case "queued", "retry delay", "host politeness", "host concurrency":
		return true
	default:
		return false
	}
}

func validCrawlerPageStage(stage string) bool {
	switch stage {
	case "robots", "http_fetch", "html_parse", "frontier_enqueue", "ingest_queue", "node_ingest":
		return true
	default:
		return false
	}
}

func validHeartbeatFetchActivity(req *crawlrpc.WorkerHeartbeat) bool {
	if req.ActiveFetches != nil &&
		*req.ActiveFetches > yagocrawlcontract.MaximumFetchWorkerConcurrency {
		return false
	}
	if req.FetchAdmissionWaiting == nil {
		return true
	}

	return req.ActiveFetches != nil && *req.FetchAdmissionWaiting <= *req.ActiveFetches
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
