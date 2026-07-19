package yagonode

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
)

func TestCrawlRunDetailSourceRendersNewestBoundedOutcomesAndLimits(t *testing.T) {
	registry := crawlruns.New(8)
	observed := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	history, err := yagocrawlcontract.NewCrawlURLOutcomeHistory([]yagocrawlcontract.CrawlURLOutcome{
		{
			Sequence:   1,
			URL:        "https://example.com/first",
			Class:      yagocrawlcontract.CrawlURLOutcomeFetched,
			ObservedAt: observed,
		},
		{
			Sequence:   2,
			URL:        "https://example.com/broken.pdf",
			Class:      yagocrawlcontract.CrawlURLOutcomeFailed,
			ObservedAt: observed.Add(time.Second),
			HTTPStatus: 200,
			Reason:     "content parser produced no indexable document",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.Record(t.Context(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-1", WorkerID: "worker", ProfileName: "Docs",
		State: yagocrawlcontract.CrawlRunRunning, RecentOutcomes: history,
		MaxPagesPerHost: 250, MaxPagesPerRun: 900, LimitsKnown: true,
	})
	source := crawlRunDetailSource{
		runs: registry,
		now:  func() time.Time { return observed.Add(time.Minute) },
	}
	detail, err := source.CrawlRunDetail(t.Context(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Run.MaxPagesPerHost != "250" || detail.Run.MaxPagesPerRun != "900" {
		t.Fatalf("detail limits = %q/%q", detail.Run.MaxPagesPerHost, detail.Run.MaxPagesPerRun)
	}
	if len(detail.Outcomes) != 2 || detail.Outcomes[0].URL != "https://example.com/broken.pdf" ||
		detail.Outcomes[0].HTTPStatus != "200" ||
		detail.Outcomes[0].Reason != "content parser produced no indexable document" ||
		detail.Outcomes[1].HTTPStatus != "Unavailable" {
		t.Fatalf("detail outcomes = %+v", detail.Outcomes)
	}
}

func TestCrawlRunDetailSourceRejectsUnknownRun(t *testing.T) {
	source := newCrawlRunDetailSource(crawlruns.New(8))
	if _, err := source.CrawlRunDetail(t.Context(), "missing"); err == nil {
		t.Fatal("missing crawl run detail succeeded")
	}
	if newCrawlRunDetailSource(nil) != nil {
		t.Fatal("crawl run detail source exists without a registry")
	}
}
