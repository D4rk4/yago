package yagonode

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

func TestAttachCrawlRunObserverRecordsMetricsAndEvent(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	collector := metrics.NewCrawlRunMetrics(prometheus.NewRegistry())
	recorder := events.NewRecorder(events.DefaultCapacity)
	attachCrawlRunObserver(runtime, collector, recorder)

	// A running report exercises the non-terminal path (gauge only, no event); the
	// finished report then folds the tally into the counters and logs the run.
	runtime.runRegistry().Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID:       "aa",
		ProfileName: "news",
		State:       yagocrawlcontract.CrawlRunRunning,
	})
	if got := recorder.Recent(10); len(got) != 0 {
		t.Fatalf("running report logged %d events, want none", len(got))
	}

	runtime.runRegistry().Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID:       "aa",
		ProfileName: "news",
		State:       yagocrawlcontract.CrawlRunFinished,
		Tally:       yagocrawlcontract.CrawlRunTally{Fetched: 4, Indexed: 3, Failed: 1},
	})

	recent := recorder.Recent(10)
	if len(recent) == 0 {
		t.Fatal("no event recorded for finished run")
	}
	ev := recent[0]
	if ev.Category != events.CategoryCrawl || ev.Name != "crawl.run.finished" {
		t.Fatalf("event = %+v, want crawl category / crawl.run.finished", ev)
	}
	if ev.Severity != events.SeverityWarn {
		t.Fatalf("severity = %v, want warn (run had failures)", ev.Severity)
	}
}

func TestAttachCrawlRunObserverIgnoresRuntimeWithoutRegistry(t *testing.T) {
	// bareCrawlProcess implements crawlProcess but exposes no run registry, so the
	// observer must attach nothing and never panic.
	attachCrawlRunObserver(
		bareCrawlProcess{},
		metrics.NewCrawlRunMetrics(prometheus.NewRegistry()),
		events.NewRecorder(events.DefaultCapacity),
	)
}

func TestAttachCrawlRunObserverNoCollectorOrRecorder(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	attachCrawlRunObserver(runtime, nil, nil)

	// With neither target, no observer is registered, so a terminal report is a
	// no-op that must not panic.
	runtime.runRegistry().Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "bb",
		State: yagocrawlcontract.CrawlRunFinished,
	})
}

func TestRecordCrawlRunEventSeverityAndName(t *testing.T) {
	cases := []struct {
		name     string
		run      crawlruns.Run
		wantName string
		wantSev  events.Severity
	}{
		{
			name:     "clean finish",
			run:      crawlruns.Run{State: yagocrawlcontract.CrawlRunFinished},
			wantName: "crawl.run.finished",
			wantSev:  events.SeverityInfo,
		},
		{
			name: "finish with failures",
			run: crawlruns.Run{
				State: yagocrawlcontract.CrawlRunFinished,
				Tally: yagocrawlcontract.CrawlRunTally{Failed: 1},
			},
			wantName: "crawl.run.finished",
			wantSev:  events.SeverityWarn,
		},
		{
			name:     "cancelled",
			run:      crawlruns.Run{State: yagocrawlcontract.CrawlRunCancelled},
			wantName: "crawl.run.cancelled",
			wantSev:  events.SeverityWarn,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := events.NewRecorder(events.DefaultCapacity)
			recordCrawlRunEvent(recorder, tc.run)
			recent := recorder.Recent(1)
			if len(recent) != 1 {
				t.Fatalf("events = %d, want 1", len(recent))
			}
			if recent[0].Name != tc.wantName || recent[0].Severity != tc.wantSev {
				t.Fatalf("event = %+v, want name=%s sev=%v", recent[0], tc.wantName, tc.wantSev)
			}
		})
	}
}

func TestRecordCrawlRunEventNilRecorder(t *testing.T) {
	recordCrawlRunEvent(nil, crawlruns.Run{State: yagocrawlcontract.CrawlRunFinished})
}

func TestCrawlRunLabelPrecedence(t *testing.T) {
	cases := []struct {
		run  crawlruns.Run
		want string
	}{
		{crawlruns.Run{ProfileName: "n", ProfileHandle: "h", RunID: "r"}, "n"},
		{crawlruns.Run{ProfileHandle: "h", RunID: "r"}, "h"},
		{crawlruns.Run{RunID: "r"}, "r"},
	}
	for _, tc := range cases {
		if got := crawlRunLabel(tc.run); got != tc.want {
			t.Fatalf("label(%+v) = %q, want %q", tc.run, got, tc.want)
		}
	}
}
