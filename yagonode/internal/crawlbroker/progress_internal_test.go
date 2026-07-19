package crawlbroker

import (
	"context"
	"encoding/hex"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type recordingProgressSink struct {
	last        yagocrawlcontract.CrawlRunProgress
	identity    []byte
	n           int
	terminalErr error
	confirmErr  error
	confirmN    int
}

func (s *recordingProgressSink) Record(_ context.Context, p yagocrawlcontract.CrawlRunProgress) {
	s.last = p
	s.n++
}

func (s *recordingProgressSink) RecordTerminal(
	_ context.Context,
	identity []byte,
	progress yagocrawlcontract.CrawlRunProgress,
) error {
	if s.terminalErr != nil {
		return s.terminalErr
	}
	s.identity = append([]byte(nil), identity...)
	s.Record(context.Background(), progress)

	return nil
}

func (s *recordingProgressSink) ConfirmTerminalDelivery(context.Context, []byte) error {
	if s.confirmErr != nil {
		return s.confirmErr
	}
	s.confirmN++

	return nil
}

func TestReportProgressTranslatesAndForwards(t *testing.T) {
	sink := &recordingProgressSink{}
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "progress", "worker-1", testWorkerSessionID)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "worker-1", testWorkerSessionID)
	server.progress = sink

	runID := []byte("admin")
	pagesPerMinute := uint32(30)
	maxPagesPerHost := int64(250)
	maxPagesPerRun := uint64(900)
	_, err := server.ReportProgress(context.Background(), &crawlrpc.CrawlProgressReport{
		WorkerId:        "worker-1",
		WorkerSessionId: testWorkerSessionID,
		LeaseId:         leaseID,
		RunId:           runID,
		ProfileHandle:   "h",
		ProfileName:     "docs",
		State:           crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED,
		Tally: &crawlrpc.CrawlRunTally{
			Fetched: 5, Indexed: 4, Failed: 1, RobotsDenied: 2, Duplicates: 3, Pending: 6,
		},
		PagesPerMinute:  &pagesPerMinute,
		MaxPagesPerHost: &maxPagesPerHost,
		MaxPagesPerRun:  &maxPagesPerRun,
	})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if sink.n != 1 {
		t.Fatalf("sink calls = %d, want 1", sink.n)
	}
	got := sink.last
	if got.RunID != hex.EncodeToString(runID) {
		t.Fatalf("run id = %q", got.RunID)
	}
	if got.WorkerID != "worker-1" || got.ProfileHandle != "h" || got.ProfileName != "docs" {
		t.Fatalf("meta = %+v", got)
	}
	if got.State != yagocrawlcontract.CrawlRunFinished {
		t.Fatalf("state = %q", got.State)
	}
	wantTally := yagocrawlcontract.CrawlRunTally{
		Fetched: 5, Indexed: 4, Failed: 1, RobotsDenied: 2, Duplicates: 3, Pending: 6,
	}
	if got.Tally != wantTally {
		t.Fatalf("tally = %+v, want %+v", got.Tally, wantTally)
	}
	if !got.RateKnown || got.PagesPerMinute != 30 {
		t.Fatalf("rate = %d/%v, want known 30", got.PagesPerMinute, got.RateKnown)
	}
	if !got.LimitsKnown || got.MaxPagesPerHost != 250 || got.MaxPagesPerRun != 900 {
		t.Fatalf("limits = %d/%d/%v", got.MaxPagesPerHost, got.MaxPagesPerRun, got.LimitsKnown)
	}
}

func TestReportProgressRejectsIncompleteAndInvalidRunLimits(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "progress-limits", "worker", testWorkerSessionID)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	maximum := uint64(10)
	request := &crawlrpc.CrawlProgressReport{
		WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
		LeaseId: leaseID, RunId: []byte("run"), MaxPagesPerRun: &maximum,
	}
	if _, err := server.ReportProgress(t.Context(), request); err == nil {
		t.Fatal("incomplete run limits were accepted")
	}
	perHost := int64(0)
	request.MaxPagesPerHost = &perHost
	if _, err := server.ReportProgress(t.Context(), request); err == nil {
		t.Fatal("zero per-host limit was accepted")
	}
}

func TestReportProgressRejectsInvalidWireEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*crawlrpc.CrawlProgressReport)
	}{
		{
			name: "URL outcome",
			mutate: func(report *crawlrpc.CrawlProgressReport) {
				report.RecentOutcomes = []*crawlrpc.CrawlURLOutcome{nil}
			},
		},
		{
			name: "run limits",
			mutate: func(report *crawlrpc.CrawlProgressReport) {
				maximum := uint64(10)
				report.MaxPagesPerRun = &maximum
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queue := memQueue(t)
			leaseID := leaseOneForSession(
				t,
				queue,
				"invalid-wire-evidence-"+test.name,
				"worker",
				testWorkerSessionID,
			)
			server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
			activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
			report := &crawlrpc.CrawlProgressReport{
				WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
				LeaseId: leaseID, RunId: []byte("admin"),
			}
			test.mutate(report)
			if _, err := server.ReportProgress(
				t.Context(),
				report,
			); status.Code(
				err,
			) != codes.InvalidArgument {
				t.Fatalf("invalid wire evidence status = %v", status.Code(err))
			}
		})
	}
}

func TestProgressFromReportPreservesUnknownRate(t *testing.T) {
	t.Parallel()

	got := progressFromReport(&crawlrpc.CrawlProgressReport{})
	if got.RateKnown || got.PagesPerMinute != 0 {
		t.Fatalf("rate = %d/%v, want unknown", got.PagesPerMinute, got.RateKnown)
	}
}

func TestRunningProgressRecordsForCurrentLeaseOwner(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "running-progress", "worker", testWorkerSessionID)
	sink := &recordingProgressSink{}
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	server.progress = sink
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	if _, err := server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
		WorkerId:        "worker",
		WorkerSessionId: testWorkerSessionID,
		LeaseId:         leaseID,
		RunId:           []byte("admin"),
		State:           crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
	}); err != nil {
		t.Fatalf("report running progress: %v", err)
	}
	if sink.n != 1 || sink.last.State != yagocrawlcontract.CrawlRunRunning {
		t.Fatalf("running progress = %d/%+v", sink.n, sink.last)
	}
}

func TestRunStateFromProtoMapsAllStates(t *testing.T) {
	cases := map[crawlrpc.CrawlRunState]yagocrawlcontract.CrawlRunState{
		crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING:     yagocrawlcontract.CrawlRunRunning,
		crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED:    yagocrawlcontract.CrawlRunFinished,
		crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED:   yagocrawlcontract.CrawlRunCancelled,
		crawlrpc.CrawlRunState_CRAWL_RUN_STATE_UNSPECIFIED: yagocrawlcontract.CrawlRunRunning,
	}
	for in, want := range cases {
		if got := runStateFromProto(in); got != want {
			t.Fatalf("state %v -> %q, want %q", in, got, want)
		}
	}
}

func TestTallyFromProtoNilSafe(t *testing.T) {
	if got := tallyFromProto(nil); got != (yagocrawlcontract.CrawlRunTally{}) {
		t.Fatalf("nil tally = %+v, want zero", got)
	}
}

func TestNoopProgressSinkRecords(t *testing.T) {
	sink := noopProgressSink{}
	sink.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{RunID: "x"})
	if err := sink.RecordTerminal(
		context.Background(),
		make([]byte, 32),
		yagocrawlcontract.CrawlRunProgress{RunID: "x"},
	); err != nil {
		t.Fatalf("record terminal: %v", err)
	}
	if err := sink.ConfirmTerminalDelivery(context.Background(), make([]byte, 32)); err != nil {
		t.Fatalf("confirm terminal: %v", err)
	}
}
