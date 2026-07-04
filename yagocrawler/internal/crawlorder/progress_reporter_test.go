package crawlorder_test

import (
	"context"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagocrawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yagocrawler/internal/crawlorder"
	"github.com/D4rk4/yago/yagocrawler/internal/frontier"
)

type captureReporter struct {
	mu      sync.Mutex
	reports []crawlorder.RunReport
}

func (r *captureReporter) ReportRun(_ context.Context, report crawlorder.RunReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports = append(r.reports, report)
}

func (r *captureReporter) snapshot() []crawlorder.RunReport {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]crawlorder.RunReport(nil), r.reports...)
}

func TestConsumerReportsRunLifecycle(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](4)
	f := frontier.NewFrontier(8, nil)
	reporter := &captureReporter{}
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f).WithProgressReporter(reporter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:            "Example",
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	acked := make(chan struct{})
	delivery := crawlorder.CrawlOrderDelivery{
		Order: yagocrawlcontract.CrawlOrder{
			Provenance: []byte("run-1"),
			Profile:    profile,
			Requests: []yagocrawlcontract.CrawlRequest{
				{URL: "https://example.com/", ProfileHandle: profile.Handle},
			},
		},
		Ack: func(context.Context) error { close(acked); return nil },
	}
	if err := queue.Publish(ctx, delivery); err != nil {
		t.Fatalf("publish delivery: %v", err)
	}

	select {
	case job := <-f.Jobs():
		f.Done(job)
	case <-time.After(3 * time.Second):
		t.Fatal("frontier never received seeded job")
	}
	select {
	case <-acked:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery never acked after run finished")
	}

	reports := reporter.snapshot()
	if len(reports) != 2 {
		t.Fatalf("reports = %+v, want running then finished", reports)
	}
	if reports[0].State != yagocrawlcontract.CrawlRunRunning ||
		string(reports[0].Provenance) != "run-1" ||
		reports[0].ProfileName != "Example" ||
		reports[0].Tally.Pending != 1 {
		t.Fatalf("first report = %+v, want running run-1 pending 1", reports[0])
	}
	if reports[1].State != yagocrawlcontract.CrawlRunFinished {
		t.Fatalf("second report = %+v, want finished", reports[1])
	}
}

type fakeProgressClient struct {
	mu   sync.Mutex
	last *crawlrpc.CrawlProgressReport
}

func (c *fakeProgressClient) ReportProgress(
	_ context.Context,
	in *crawlrpc.CrawlProgressReport,
	_ ...grpc.CallOption,
) (*crawlrpc.CrawlProgressAck, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = in

	return &crawlrpc.CrawlProgressAck{}, nil
}

func TestGRPCProgressReporterMapsReportToProto(t *testing.T) {
	client := &fakeProgressClient{}
	reporter := crawlorder.NewGRPCProgressReporter(client, "worker-9")

	reporter.ReportRun(context.Background(), crawlorder.RunReport{
		Provenance:    []byte("run-1"),
		ProfileHandle: "h1",
		ProfileName:   "Profile One",
		State:         yagocrawlcontract.CrawlRunFinished,
		Tally:         yagocrawlcontract.CrawlRunTally{Fetched: 3, Indexed: 2, Pending: 1},
	})

	got := client.last
	if got == nil {
		t.Fatal("no report sent")
	}
	if got.GetWorkerId() != "worker-9" ||
		string(got.GetRunId()) != "run-1" ||
		got.GetProfileHandle() != "h1" ||
		got.GetProfileName() != "Profile One" {
		t.Fatalf("report identity = %+v", got)
	}
	if got.GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED {
		t.Fatalf("state = %v, want finished", got.GetState())
	}
	if got.GetTally().GetFetched() != 3 ||
		got.GetTally().GetIndexed() != 2 ||
		got.GetTally().GetPending() != 1 {
		t.Fatalf("tally = %+v", got.GetTally())
	}
}
