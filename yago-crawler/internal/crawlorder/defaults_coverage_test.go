package crawlorder

import (
	"context"
	"errors"
	"testing"
	"time"

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

type erroringProgressClient struct {
	observed chan struct{}
}

func (c erroringProgressClient) ReportProgress(
	context.Context,
	*crawlrpc.CrawlProgressReport,
	...grpc.CallOption,
) (*crawlrpc.CrawlProgressAck, error) {
	select {
	case c.observed <- struct{}{}:
	default:
	}

	return nil, errors.New("report boom")
}

func TestGRPCProgressReporterDropsFailedReport(t *testing.T) {
	observed := make(chan struct{}, 1)
	reporter := NewGRPCProgressReporter(erroringProgressClient{observed: observed}, "worker-err")
	reporter.ReportRun(
		context.Background(),
		RunReport{State: yagocrawlcontract.CrawlRunRunning},
	)
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("progress failure path was not attempted")
	}
	if err := reporter.Close(t.Context()); err != nil {
		t.Fatalf("close reporter: %v", err)
	}
}
