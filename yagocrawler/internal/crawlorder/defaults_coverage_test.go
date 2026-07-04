package crawlorder

import (
	"context"
	"errors"
	"testing"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestProtoRunStateMapsEveryState(t *testing.T) {
	cases := map[yagocrawlcontract.CrawlRunState]crawlrpc.CrawlRunState{
		yagocrawlcontract.CrawlRunRunning:        crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
		yagocrawlcontract.CrawlRunFinished:       crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED,
		yagocrawlcontract.CrawlRunCancelled:      crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED,
		yagocrawlcontract.CrawlRunState("other"): crawlrpc.CrawlRunState_CRAWL_RUN_STATE_UNSPECIFIED,
	}
	for state, want := range cases {
		if got := protoRunState(state); got != want {
			t.Errorf("protoRunState(%q) = %v, want %v", state, got, want)
		}
	}
}

type erroringProgressClient struct{}

func (erroringProgressClient) ReportProgress(
	context.Context,
	*crawlrpc.CrawlProgressReport,
	...grpc.CallOption,
) (*crawlrpc.CrawlProgressAck, error) {
	return nil, errors.New("report boom")
}

// TestGRPCProgressReporterDropsFailedReport covers the best-effort path: a failed
// report is logged and swallowed rather than propagated to the caller.
func TestGRPCProgressReporterDropsFailedReport(t *testing.T) {
	reporter := NewGRPCProgressReporter(erroringProgressClient{}, "worker-err")
	reporter.ReportRun(
		context.Background(),
		RunReport{State: yagocrawlcontract.CrawlRunRunning},
	)
}
