package crawlorder

import (
	"context"
	"log/slog"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

const msgProgressReportFailed = "crawl progress report failed"

// DefaultProgressReportTimeout bounds a single best-effort progress RPC so a slow
// or unreachable node cannot stall order processing on it.
const DefaultProgressReportTimeout = 5 * time.Second

var progressReportTimeout = DefaultProgressReportTimeout

// ProgressClient is the slice of the node's CrawlExchange client the reporter
// needs: the unary call that records a worker's run progress.
type ProgressClient interface {
	ReportProgress(
		ctx context.Context,
		in *crawlrpc.CrawlProgressReport,
		opts ...grpc.CallOption,
	) (*crawlrpc.CrawlProgressAck, error)
}

// GRPCProgressReporter reports run lifecycle snapshots to the node over the crawl
// exchange. A failed report is logged and dropped: the next report carries an
// absolute snapshot, so the node self-corrects without the worker retrying.
type GRPCProgressReporter struct {
	client   ProgressClient
	workerID string
}

func NewGRPCProgressReporter(client ProgressClient, workerID string) *GRPCProgressReporter {
	return &GRPCProgressReporter{client: client, workerID: workerID}
}

func (r *GRPCProgressReporter) ReportRun(ctx context.Context, report RunReport) {
	callCtx, cancel := context.WithTimeout(ctx, progressReportTimeout)
	defer cancel()
	if _, err := r.client.ReportProgress(callCtx, &crawlrpc.CrawlProgressReport{
		WorkerId:      r.workerID,
		RunId:         report.Provenance,
		ProfileHandle: report.ProfileHandle,
		ProfileName:   report.ProfileName,
		State:         protoRunState(report.State),
		Tally:         protoRunTally(report.Tally),
	}); err != nil {
		slog.WarnContext(ctx, msgProgressReportFailed, slog.Any("error", err))
	}
}

func protoRunState(state yagocrawlcontract.CrawlRunState) crawlrpc.CrawlRunState {
	switch state {
	case yagocrawlcontract.CrawlRunRunning:
		return crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING
	case yagocrawlcontract.CrawlRunFinished:
		return crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED
	case yagocrawlcontract.CrawlRunCancelled:
		return crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED
	default:
		return crawlrpc.CrawlRunState_CRAWL_RUN_STATE_UNSPECIFIED
	}
}

func protoRunTally(tally yagocrawlcontract.CrawlRunTally) *crawlrpc.CrawlRunTally {
	return &crawlrpc.CrawlRunTally{
		Fetched:      tally.Fetched,
		Indexed:      tally.Indexed,
		Failed:       tally.Failed,
		RobotsDenied: tally.RobotsDenied,
		Duplicates:   tally.Duplicates,
		Pending:      tally.Pending,
	}
}
