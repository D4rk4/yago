package crawlorder_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlorder"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
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
	f := frontier.NewFrontier(8, nil, frontier.WithDefaultRunRate(30))
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

	takeCtx, cancelTake := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancelTake()
	if job, ok := f.Take(takeCtx); ok {
		f.Done(job, successfulPageOutcome())
	} else {
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
		reports[0].Tally.Pending != 1 ||
		reports[0].PagesPerMinute != 30 {
		t.Fatalf("first report = %+v, want running run-1 pending 1", reports[0])
	}
	if reports[1].State != yagocrawlcontract.CrawlRunFinished {
		t.Fatalf("second report = %+v, want finished", reports[1])
	}
}

type stubRunTally struct {
	mu        sync.Mutex
	tally     yagocrawlcontract.CrawlRunTally
	forgotten [][]byte
}

func (s *stubRunTally) Snapshot([]byte) yagocrawlcontract.CrawlRunTally {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.tally
}

func (s *stubRunTally) Restore(_ []byte, tally yagocrawlcontract.CrawlRunTally) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tally.Pending = 0
	s.tally = tally
}

func (s *stubRunTally) Forget(provenance []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forgotten = append(s.forgotten, append([]byte(nil), provenance...))
}

func (s *stubRunTally) forgottenRuns() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([][]byte(nil), s.forgotten...)
}

func TestConsumerReportsRunOutcomeTally(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](4)
	f := frontier.NewFrontier(8, nil)
	reporter := &captureReporter{}
	tally := &stubRunTally{tally: yagocrawlcontract.CrawlRunTally{
		Fetched: 2,
		Indexed: 1,
		Failed:  1,
	}}
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f).
		WithProgressReporter(reporter).
		WithRunTally(tally)

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
			Provenance: []byte("run-9"),
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

	takeCtx, cancelTake := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancelTake()
	if job, ok := f.Take(takeCtx); ok {
		f.Done(job, successfulPageOutcome())
	} else {
		t.Fatal("frontier never received seeded job")
	}
	select {
	case <-acked:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery never acked after run finished")
	}

	reports := reporter.snapshot()
	finished := reports[len(reports)-1]
	if finished.State != yagocrawlcontract.CrawlRunFinished {
		t.Fatalf("last report = %+v, want finished", finished)
	}
	if finished.Tally.Fetched != 2 ||
		finished.Tally.Indexed != 1 ||
		finished.Tally.Failed != 1 ||
		finished.Tally.Pending != 0 {
		t.Fatalf("finish tally = %+v, want fetched 2 indexed 1 failed 1 pending 0",
			finished.Tally)
	}
	forgotten := tally.forgottenRuns()
	if len(forgotten) != 1 || string(forgotten[0]) != "run-9" {
		t.Fatalf("forgotten runs = %v, want [run-9] after finish", forgotten)
	}
}

type fakeProgressClient struct {
	mu       sync.Mutex
	last     *crawlrpc.CrawlProgressReport
	reported chan struct{}
	err      error
}

func (c *fakeProgressClient) ReportProgress(
	_ context.Context,
	in *crawlrpc.CrawlProgressReport,
	_ ...grpc.CallOption,
) (*crawlrpc.CrawlProgressAck, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = in
	if c.reported != nil {
		select {
		case c.reported <- struct{}{}:
		default:
		}
	}
	if c.err != nil {
		return nil, c.err
	}

	return &crawlrpc.CrawlProgressAck{}, nil
}

func TestGRPCProgressReporterMapsReportToProto(t *testing.T) {
	client := &fakeProgressClient{reported: make(chan struct{}, 1)}
	reporter := crawlorder.NewGRPCProgressReporter(client, "worker-9")

	reporter.ReportRun(context.Background(), crawlorder.RunReport{
		Provenance:     []byte("run-1"),
		ProfileHandle:  "h1",
		ProfileName:    "Profile One",
		State:          yagocrawlcontract.CrawlRunFinished,
		Tally:          yagocrawlcontract.CrawlRunTally{Fetched: 3, Indexed: 2, Pending: 1},
		PagesPerMinute: 45,
	})

	select {
	case <-client.reported:
	case <-time.After(time.Second):
		t.Fatal("no report sent")
	}
	client.mu.Lock()
	got := client.last
	client.mu.Unlock()
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
	if got.PagesPerMinute == nil || got.GetPagesPerMinute() != 45 {
		t.Fatalf("pages per minute = %v, want known 45", got.PagesPerMinute)
	}
	if err := reporter.Close(t.Context()); err != nil {
		t.Fatalf("close reporter: %v", err)
	}
}

func TestGRPCProgressReporterFencesLeaseSession(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	if err := registry.Track("lease"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	registry.Renew(started, time.Hour, []string{"lease"}, []string{"lease"})
	leaseContext, ok := registry.Context("lease")
	if !ok {
		t.Fatal("confirmed lease context missing")
	}
	client := &fakeProgressClient{
		reported: make(chan struct{}, 1),
		err:      status.Error(codes.FailedPrecondition, "lease superseded"),
	}
	reporter := crawlorder.NewGRPCProgressReporter(
		client,
		"worker",
		crawlorder.WithProgressLeaseSession("session", registry),
	)
	reporter.ReportRun(t.Context(), crawlorder.RunReport{
		LeaseID: "lease", Provenance: []byte("run"),
		State: yagocrawlcontract.CrawlRunFinished,
	})
	select {
	case <-client.reported:
	case <-time.After(time.Second):
		t.Fatal("lease progress was not submitted")
	}
	client.mu.Lock()
	got := client.last
	client.mu.Unlock()
	if got.GetLeaseId() != "lease" || got.GetWorkerId() != "worker" ||
		got.GetWorkerSessionId() != "session" {
		t.Fatalf("lease progress identity = %+v", got)
	}
	deadline := time.Now().Add(time.Second)
	for registry.Confirmed("lease") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if registry.Confirmed("lease") {
		t.Fatal("failed-precondition progress retained lease grant")
	}
	if !errors.Is(context.Cause(leaseContext), crawllease.ErrLeaseLost) {
		t.Fatalf("lease cancellation cause = %v", context.Cause(leaseContext))
	}
	if err := reporter.Close(t.Context()); err != nil {
		t.Fatalf("close reporter: %v", err)
	}
}
