package crawlbroker

import (
	"context"
	"encoding/hex"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

// ProgressSink receives crawl run progress reported by workers. The broker
// forwards decoded reports to it, so the node's run registry stays decoupled from
// the gRPC contract.
type ProgressSink interface {
	Record(ctx context.Context, progress yagocrawlcontract.CrawlRunProgress)
}

type noopProgressSink struct{}

func (noopProgressSink) Record(context.Context, yagocrawlcontract.CrawlRunProgress) {}

func progressFromReport(report *crawlrpc.CrawlProgressReport) yagocrawlcontract.CrawlRunProgress {
	return yagocrawlcontract.CrawlRunProgress{
		RunID:          hex.EncodeToString(report.GetRunId()),
		WorkerID:       report.GetWorkerId(),
		ProfileHandle:  report.GetProfileHandle(),
		ProfileName:    report.GetProfileName(),
		State:          runStateFromProto(report.GetState()),
		Tally:          tallyFromProto(report.GetTally()),
		PagesPerMinute: report.GetPagesPerMinute(),
		RateKnown:      report.PagesPerMinute != nil,
	}
}

func runStateFromProto(state crawlrpc.CrawlRunState) yagocrawlcontract.CrawlRunState {
	switch state {
	case crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED:
		return yagocrawlcontract.CrawlRunFinished
	case crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED:
		return yagocrawlcontract.CrawlRunCancelled
	default:
		return yagocrawlcontract.CrawlRunRunning
	}
}

func tallyFromProto(tally *crawlrpc.CrawlRunTally) yagocrawlcontract.CrawlRunTally {
	return yagocrawlcontract.CrawlRunTally{
		Fetched:      tally.GetFetched(),
		Indexed:      tally.GetIndexed(),
		Failed:       tally.GetFailed(),
		RobotsDenied: tally.GetRobotsDenied(),
		Duplicates:   tally.GetDuplicates(),
		Pending:      tally.GetPending(),
	}
}
