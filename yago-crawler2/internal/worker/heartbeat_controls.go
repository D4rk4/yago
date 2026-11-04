package worker

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

const (
	messageWorkerCountChanged      = "crawler worker count changed"
	messageRunLimitChanged         = "crawler active run limit changed"
	messageWaitingPageReportFailed = "crawler waiting page report failed"
)

func (worker *Worker) waitingPageStatuses(ctx context.Context) []*crawlrpc.WaitingCrawlPage {
	statuses, err := worker.waitingPageReports.snapshot(ctx, worker.frontier)
	if err != nil {
		slog.WarnContext(ctx, messageWaitingPageReportFailed, slog.Any("error", err))
		return nil
	}
	return statuses
}

func (worker *Worker) sendHeartbeat(
	ctx context.Context,
	leaseIDs []string,
	activeFetches uint32,
	confirmDeliveries bool,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	worker.heartbeatMutex.Lock()
	defer worker.heartbeatMutex.Unlock()
	acknowledged := append([]uint64(nil), worker.pendingDirectiveAcknowledgments...)
	result, err := worker.exchange.Heartbeat(
		ctx,
		leaseIDs,
		activeFetches,
		worker.activePageStatuses(),
		worker.waitingPageStatuses(ctx),
		confirmDeliveries,
		acknowledged,
	)
	if err != nil {
		return nil, err
	}
	worker.pendingDirectiveAcknowledgments = nil
	for _, directive := range result.GetDirectives() {
		if worker.applyHeartbeatDirective(ctx, directive) {
			worker.pendingDirectiveAcknowledgments = append(
				worker.pendingDirectiveAcknowledgments,
				directive.GetDirectiveId(),
			)
		}
	}
	return result, nil
}

func (worker *Worker) applyHeartbeatDirective(
	ctx context.Context,
	directive *crawlrpc.CrawlControlDirective,
) bool {
	switch directive.GetKind() {
	case crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_WORKERS:
		if !worker.fetchAdmission.SetMaximum(directive.GetFetchWorkers()) {
			return false
		}
		slog.InfoContext(
			ctx,
			messageWorkerCountChanged,
			slog.Uint64("workers", uint64(directive.GetFetchWorkers())),
		)
		return true
	case crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_ACTIVE_RUNS:
		if !worker.orders.SetMaximumActiveRuns(directive.GetMaximumActiveRuns()) {
			return false
		}
		slog.InfoContext(
			ctx,
			messageRunLimitChanged,
			slog.Uint64("maximumActiveRuns", uint64(directive.GetMaximumActiveRuns())),
		)
		return true
	case crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_MAXIMUM_REDIRECTS:
		return worker.fetcher.SetMaximumRedirects(int(directive.GetMaximumRedirects()))
	case crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_PROCESS_RATE,
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_AUTOMATIC_DISCOVERY_PRIORITY:
		return true
	default:
		return false
	}
}
