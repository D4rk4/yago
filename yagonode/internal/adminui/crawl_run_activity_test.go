package adminui

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAttachCrawlerRunActivityDistinguishesActiveWaitingAndUnavailable(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	runs := []CrawlRunView{
		{RunID: "run-a", Worker: "worker-a", State: "running"},
		{RunID: "run-b", Worker: "worker-b", State: "running"},
		{RunID: "run-c", Worker: "worker-missing", State: "running"},
		{RunID: "run-d", Worker: "worker-unknown", State: "running"},
	}
	pulse := CrawlerPulse{
		Workers: []CrawlerWorkerPulse{
			{
				WorkerID:           "worker-a",
				ActiveFetchesKnown: true,
				ObservedAt:         now,
				ActivePages: []CrawlerActivePage{{
					RunID: "run-a", URL: "https://example.org/current", Stage: "http_fetch",
					ElapsedMilliseconds: 1200,
				}},
				WaitingPages: []CrawlerWaitingPage{{
					RunID: "run-a", URL: "https://example.org/next", State: "queued",
					Reason: "host politeness", AvailableAt: now.Add(2 * time.Second),
				}},
			},
			{WorkerID: "worker-b", ActiveFetchesKnown: true, ObservedAt: now},
			{WorkerID: "worker-unknown", ObservedAt: now, ActivePages: []CrawlerActivePage{{
				RunID: "run-d", URL: "https://example.org/stale", Stage: "http_fetch",
			}}},
		},
	}

	attachCrawlerRunActivity(runs, pulse, now)
	if !runs[0].URLActivityKnown || len(runs[0].URLActivity) != 2 ||
		runs[0].URLActivity[0].Kind != "fetching" ||
		runs[0].URLActivity[1].Kind != "queued" ||
		runs[0].URLActivity[1].Availability != "in 2s" {
		t.Fatalf("run activity = %#v", runs[0])
	}
	if !runs[1].URLActivityKnown || len(runs[1].URLActivity) != 0 {
		t.Fatalf("empty current run activity = %#v", runs[1])
	}
	if runs[2].URLActivityKnown || len(runs[2].URLActivity) != 0 {
		t.Fatalf("missing worker activity = %#v", runs[2])
	}
	if runs[3].URLActivityKnown || len(runs[3].URLActivity) != 0 {
		t.Fatalf("unknown worker activity = %#v", runs[3])
	}
}

func TestConsoleCrawlRendersActiveAndWaitingURLs(t *testing.T) {
	monitor := sampleMonitor()
	monitor.Runs[0].Worker = "worker-a"
	console := New(Options{
		Crawl:   &fakeCrawl{},
		Monitor: fakeMonitor{snap: monitor},
		CrawlerPulse: fixedCrawlerPulse{pulse: CrawlerPulse{
			Available: true,
			Workers: []CrawlerWorkerPulse{{
				WorkerID:           "worker-a",
				ActiveFetchesKnown: true,
				ObservedAt:         time.Now(),
				ActivePages: []CrawlerActivePage{{
					RunID: "run-abc", URL: "https://example.org/current", Stage: "http_fetch",
					ElapsedMilliseconds: 2300,
				}},
				WaitingPages: []CrawlerWaitingPage{{
					RunID: "run-abc", URL: "https://example.org/next", State: "queued",
					Reason: "host politeness", AvailableAt: time.Now().Add(time.Second),
				}},
			}},
		}},
	})

	got := do(t, console, "/admin/crawl")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		"Current and next URLs", "fetching", "fetching page", "https://example.org/current",
		"queued", "host politeness", "https://example.org/next",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("crawl page missing %q", want)
		}
	}
}
