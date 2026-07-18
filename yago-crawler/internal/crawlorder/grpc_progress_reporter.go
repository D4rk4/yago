package crawlorder

import (
	"context"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

// ProgressClient is the slice of the node's CrawlExchange client the reporter
// needs: the unary call that records a worker's run progress.
type ProgressClient interface {
	ReportProgress(
		ctx context.Context,
		in *crawlrpc.CrawlProgressReport,
		opts ...grpc.CallOption,
	) (*crawlrpc.CrawlProgressAck, error)
}

type grpcProgressReporter struct {
	delivery *progressDeliveryQueue
}

type GRPCProgressReporterOption func(*progressDeliveryQueue)

func WithProgressLeaseSession(
	workerSessionID string,
	leaseGrants *crawllease.GrantRegistry,
) GRPCProgressReporterOption {
	return func(queue *progressDeliveryQueue) {
		queue.workerSessionID = workerSessionID
		queue.leaseGrants = leaseGrants
	}
}

func NewGRPCProgressReporter(
	client ProgressClient,
	workerID string,
	options ...GRPCProgressReporterOption,
) *grpcProgressReporter {
	return &grpcProgressReporter{
		delivery: newProgressDeliveryQueue(
			client,
			workerID,
			defaultProgressDeliveryPolicy(),
			options...,
		),
	}
}

func (r *grpcProgressReporter) ReportRun(ctx context.Context, report RunReport) {
	r.delivery.enqueue(ctx, report)
}

func (r *grpcProgressReporter) Close(ctx context.Context) error {
	return r.delivery.close(ctx)
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
