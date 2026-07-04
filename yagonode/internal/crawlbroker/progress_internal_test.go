package crawlbroker

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type recordingProgressSink struct {
	last yagocrawlcontract.CrawlRunProgress
	n    int
}

func (s *recordingProgressSink) Record(_ context.Context, p yagocrawlcontract.CrawlRunProgress) {
	s.last = p
	s.n++
}

func TestReportProgressTranslatesAndForwards(t *testing.T) {
	sink := &recordingProgressSink{}
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	server.progress = sink

	runID := []byte{0xde, 0xad, 0xbe, 0xef}
	_, err := server.ReportProgress(context.Background(), &crawlrpc.CrawlProgressReport{
		WorkerId:      "worker-1",
		RunId:         runID,
		ProfileHandle: "h",
		ProfileName:   "docs",
		State:         crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED,
		Tally: &crawlrpc.CrawlRunTally{
			Fetched: 5, Indexed: 4, Failed: 1, RobotsDenied: 2, Duplicates: 3, Pending: 6,
		},
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
	noopProgressSink{}.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{RunID: "x"})
}
