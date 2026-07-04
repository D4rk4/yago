package adminui

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type fakeMonitor struct {
	snap CrawlMonitor
}

func (f fakeMonitor) Monitor(context.Context) CrawlMonitor {
	return f.snap
}

func sampleMonitor() CrawlMonitor {
	return CrawlMonitor{
		Runs: []CrawlRunView{{
			Profile: "news-crawl",
			State:   "running",
			Fetched: 12,
			Indexed: 9,
			Failed:  1,
			Pending: 4,
			Runtime: "2m0s",
		}},
		QueuePending: 7,
		QueueLeased:  2,
	}
}

func TestConsoleCrawlRendersMonitor(t *testing.T) {
	t.Parallel()

	console := New(Options{Crawl: &fakeCrawl{}, Monitor: fakeMonitor{snap: sampleMonitor()}})
	got := do(t, console, "/admin/crawl")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{"Crawl monitor", "news-crawl", "cds-tag--info", "7 pending, 2 leased"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("crawl page missing %q", want)
		}
	}
}

func TestConsoleCrawlMonitorPartial(t *testing.T) {
	t.Parallel()

	console := New(Options{Crawl: &fakeCrawl{}, Monitor: fakeMonitor{snap: sampleMonitor()}})
	got := do(t, console, "/admin/crawl/monitor")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, "news-crawl") {
		t.Fatal("monitor partial missing run row")
	}
	if strings.Contains(got.body, "<html") {
		t.Fatal("monitor partial should render the fragment, not the full layout")
	}
}

func TestConsoleCrawlMonitorPartialNotFoundWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Crawl: &fakeCrawl{}}), "/admin/crawl/monitor")
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404 without a monitor source", got.status)
	}
}

func TestConsoleCrawlWithoutMonitorRendersFormOnly(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Crawl: &fakeCrawl{}}), "/admin/crawl")
	if strings.Contains(got.body, "Crawl monitor") {
		t.Fatal("crawl page rendered a monitor without a monitor source")
	}
}

func TestConsoleCrawlMonitorEmptyRuns(t *testing.T) {
	t.Parallel()

	console := New(Options{Crawl: &fakeCrawl{}, Monitor: fakeMonitor{snap: CrawlMonitor{}}})
	got := do(t, console, "/admin/crawl/monitor")
	if !strings.Contains(got.body, "No crawl runs observed yet.") {
		t.Fatal("empty monitor missing the empty state")
	}
}

func TestCrawlStateTag(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"running":   "info",
		"cancelled": "warn",
		"finished":  "",
		"unknown":   "",
	}
	for state, want := range cases {
		if got := crawlStateTag(state); got != want {
			t.Fatalf("crawlStateTag(%q) = %q, want %q", state, got, want)
		}
	}
}
