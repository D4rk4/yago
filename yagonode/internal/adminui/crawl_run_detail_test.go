package adminui

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type fakeCrawlRunDetails struct {
	detail CrawlRunDetail
	err    error
}

func (source fakeCrawlRunDetails) CrawlRunDetail(
	context.Context,
	string,
) (CrawlRunDetail, error) {
	return source.detail, source.err
}

func TestConsoleRendersBoundedCrawlRunDetail(t *testing.T) {
	detail := CrawlRunDetail{
		Run: CrawlRunView{
			RunID: "run-1", Profile: "Docs", Worker: "worker-1", State: "running",
			Runtime: "2m0s", MaxPagesPerHost: "250", MaxPagesPerRun: "900",
		},
		Outcomes: []CrawlURLOutcomeView{
			{
				URL: "https://example.com/broken.pdf", Class: "failed",
				ObservedAt: "2026-07-18T12:00:00Z", HTTPStatus: "200",
				Reason: "content parser produced no indexable document",
			},
			{
				URL: "https://example.com/no-status", Class: "failed",
				ObservedAt: "2026-07-18T11:59:00Z", HTTPStatus: "Unavailable",
				Reason: "page fetch failed",
			},
		},
	}
	console := New(Options{CrawlRunDetails: fakeCrawlRunDetails{detail: detail}})
	got := do(t, console, "/admin/crawl/run?runId=run-1")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d", got.status)
	}
	for _, want := range []string{
		"Whole-run maximum (pages)", ">900<", "Per-host maximum (pages)", ">250<",
		"Recent URL outcomes", "broken.pdf", "content parser produced no indexable document",
		"Each URL has one terminal class", "Aggregate counters are not mutually exclusive",
		"one page can increment both", "no page body or raw parser error", ">Unavailable<",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("crawl run detail missing %q", want)
		}
	}
}

func TestConsoleLinksMonitorRowsToCrawlRunDetail(t *testing.T) {
	console := New(Options{
		Monitor:         fakeMonitor{snap: sampleMonitor()},
		CrawlRunDetails: fakeCrawlRunDetails{},
	})
	got := do(t, console, "/admin/crawl/monitor")
	if !strings.Contains(got.body, `href="/admin/crawl/run?runId=run-abc"`) {
		t.Fatal("crawl monitor row is not linked to run detail")
	}
}

func TestConsoleCrawlRunDetailReportsUnavailableSourceData(t *testing.T) {
	console := New(Options{CrawlRunDetails: fakeCrawlRunDetails{err: errors.New("missing")}})
	got := do(t, console, "/admin/crawl/run?runId=missing")
	if got.status != http.StatusOK ||
		!strings.Contains(got.body, "Crawl run detail is unavailable") {
		t.Fatalf("missing detail response = %d %q", got.status, got.body)
	}
}

func TestConsoleCrawlRunDetailRequiresSourceAndRunIdentity(t *testing.T) {
	withoutSource := New(Options{})
	if got := do(
		t,
		withoutSource,
		"/admin/crawl/run?runId=run-1",
	); got.status != http.StatusNotFound {
		t.Fatalf("missing source status = %d", got.status)
	}
	console := New(Options{CrawlRunDetails: fakeCrawlRunDetails{}})
	got := do(t, console, "/admin/crawl/run")
	if got.status != http.StatusOK || !strings.Contains(got.body, "Select a crawl run.") {
		t.Fatalf("missing run identity = %d %q", got.status, got.body)
	}
}
