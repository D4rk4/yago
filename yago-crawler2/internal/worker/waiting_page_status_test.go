package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler2/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestWaitingPageReporterSharesNextFrontierURLsByRun(t *testing.T) {
	workFrontier, err := frontier.Open(filepath.Join(t.TempDir(), "frontier.db"), frontier.Config{
		MaximumPendingPages: 10,
		LeaseDuration:       time.Minute,
		HostDelay:           time.Second,
		MaximumHostActive:   1,
	})
	if err != nil {
		t.Fatalf("open frontier: %v", err)
	}
	t.Cleanup(func() { _ = workFrontier.Close() })
	for _, runID := range []string{"run-a", "run-b"} {
		if _, _, err := workFrontier.Enqueue(
			context.Background(), runID, "https://"+runID+".example/next", "", 0, time.Now(),
		); err != nil {
			t.Fatalf("enqueue %s: %v", runID, err)
		}
	}
	reporter := newWaitingPageReporter()
	reporter.runStarted("run-a")
	reporter.runStarted("run-b")

	statuses, err := reporter.snapshot(context.Background(), workFrontier)
	if err != nil || len(statuses) != 2 {
		t.Fatalf("waiting page status = %#v, err %v", statuses, err)
	}
	if statuses[0].GetUrl() != "https://run-a.example/next" ||
		statuses[1].GetUrl() != "https://run-b.example/next" {
		t.Fatalf("waiting page statuses = %#v", statuses)
	}
	reporter.runFinished("run-a")
	statuses, err = reporter.snapshot(context.Background(), workFrontier)
	if err != nil || len(statuses) != 1 || statuses[0].GetRunId() != "run-b" {
		t.Fatalf("remaining waiting page status = %#v, err %v", statuses, err)
	}
}

func TestWaitingPageReporterBoundsAndRotatesActiveRunSamples(t *testing.T) {
	reporter := newWaitingPageReporter()
	if got := reporter.nextRunIDs(
		yagocrawlcontract.MaximumCrawlerWaitingPageReports,
	); len(
		got,
	) != 0 {
		t.Fatalf("empty reporter selected %d runs", len(got))
	}
	for index := 0; index <= yagocrawlcontract.MaximumCrawlerWaitingPageReports; index++ {
		reporter.runStarted(fmt.Sprintf("run-%03d", index))
	}
	first := reporter.nextRunIDs(yagocrawlcontract.MaximumCrawlerWaitingPageReports)
	second := reporter.nextRunIDs(yagocrawlcontract.MaximumCrawlerWaitingPageReports)
	if len(first) != yagocrawlcontract.MaximumCrawlerWaitingPageReports || len(second) != 1 {
		t.Fatalf("rotated sample sizes = %d and %d", len(first), len(second))
	}
	if first[0] == second[0] {
		t.Fatalf("sample did not rotate beyond its first batch: %q", first[0])
	}
}
