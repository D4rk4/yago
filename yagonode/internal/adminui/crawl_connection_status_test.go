package adminui

import (
	"strings"
	"testing"
)

func TestBuildCrawlConnectionView(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     CrawlerFetchActivitySource
		wantState  string
		wantDetail string
		wantTag    string
	}{
		{
			name:       "source unavailable",
			wantState:  "Unavailable",
			wantDetail: "Connection telemetry is unavailable.",
			wantTag:    "debug",
		},
		{
			name: "invalid source reading",
			source: fixedCrawlerFetchActivity{activity: CrawlerFetchActivity{
				ConnectedCrawlers: -1,
			}},
			wantState:  "Unavailable",
			wantDetail: "Connection telemetry is unavailable.",
			wantTag:    "debug",
		},
		{
			name: "disconnected",
			source: fixedCrawlerFetchActivity{activity: CrawlerFetchActivity{
				ConnectedCrawlers: 0,
			}},
			wantState: "Disconnected",
			wantDetail: "No crawlers are connected. " +
				"Queued crawl orders remain pending until a crawler connects.",
			wantTag: "error",
		},
		{
			name: "one crawler connected",
			source: fixedCrawlerFetchActivity{activity: CrawlerFetchActivity{
				ConnectedCrawlers: 1,
			}},
			wantState:  "Connected",
			wantDetail: "1 crawler connected.",
			wantTag:    "success",
		},
		{
			name: "multiple crawlers connected",
			source: fixedCrawlerFetchActivity{activity: CrawlerFetchActivity{
				ConnectedCrawlers: 3,
			}},
			wantState:  "Connected",
			wantDetail: "3 crawlers connected.",
			wantTag:    "success",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := buildCrawlConnectionView(test.source)
			if got.State != test.wantState || got.Detail != test.wantDetail ||
				got.Tag != test.wantTag {
				t.Fatalf("crawl connection view = %+v", got)
			}
		})
	}
}

func TestConsoleCrawlMonitorRendersCrawlerConnection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source CrawlerFetchActivitySource
		want   []string
		refuse string
	}{
		{
			name: "connected",
			source: fixedCrawlerFetchActivity{activity: CrawlerFetchActivity{
				ConnectedCrawlers: 2,
			}},
			want: []string{
				`aria-label="Crawler connection"`,
				"Crawler connection:",
				"cds-tag--success",
				">Connected<",
				"2 crawlers connected.",
			},
			refuse: "Queued crawl orders remain pending",
		},
		{
			name: "disconnected",
			source: fixedCrawlerFetchActivity{activity: CrawlerFetchActivity{
				ConnectedCrawlers: 0,
			}},
			want: []string{
				`aria-label="Crawler connection"`,
				"Crawler connection:",
				"cds-tag--error",
				">Disconnected<",
				"Queued crawl orders remain pending until a crawler connects.",
			},
			refuse: "crawlers connected.",
		},
		{
			name: "telemetry unavailable",
			want: []string{
				`aria-label="Crawler connection"`,
				"Crawler connection:",
				"cds-tag--debug",
				">Unavailable<",
				"Connection telemetry is unavailable.",
			},
			refuse: "Queued crawl orders remain pending",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			console := New(Options{
				Monitor:              fakeMonitor{snap: sampleMonitor()},
				CrawlerFetchActivity: test.source,
			})
			got := do(t, console, "/admin/crawl/monitor")
			for _, want := range test.want {
				if !strings.Contains(got.body, want) {
					t.Fatalf("crawl connection missing %q", want)
				}
			}
			if strings.Contains(got.body, test.refuse) {
				t.Fatalf("crawl connection rendered refused text %q", test.refuse)
			}
		})
	}
}
