package adminui

import (
	"strings"
	"testing"
)

type fixedCrawlerFetchActivity struct {
	activity CrawlerFetchActivity
}

func (s fixedCrawlerFetchActivity) CrawlerFetchActivity() CrawlerFetchActivity {
	return s.activity
}

func TestCrawlerSystemMonitorBuildsRuntimeStates(t *testing.T) {
	t.Parallel()

	if view := buildSystemMonitorWithCrawler(nil, nil); view.CrawlerFetchVisible {
		t.Fatalf("disabled crawler became visible: %+v", view)
	}

	idle := buildSystemMonitorWithCrawler(nil, fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			FetchLimitPerCrawler: 4,
			ActiveFetchesKnown:   true,
		},
	})
	if idle.CrawlerFetchVisible || idle.CrawlerFetchAvailable ||
		idle.CrawlerFetchValue != 0 || idle.CrawlerFetchMaximum != 0 ||
		idle.CrawlerFetchText != "" {
		t.Fatalf("idle crawler reading = %+v", idle)
	}

	single := buildSystemMonitorWithCrawler(nil, fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers:      1,
			ActiveFetches:          6,
			FetchLimitPerCrawler:   4,
			AggregateFetchCapacity: 4,
			ActiveFetchesKnown:     true,
		},
	})
	if !single.CrawlerFetchVisible || !single.CrawlerFetchAvailable ||
		single.CrawlerFetchValue != 4 || single.CrawlerFetchMaximum != 4 ||
		single.CrawlerFetchText != "6 active of 4" {
		t.Fatalf("single crawler reading = %+v", single)
	}

	multiple := buildSystemMonitorWithCrawler(nil, fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers:      3,
			ActiveFetches:          7,
			FetchLimitPerCrawler:   4,
			AggregateFetchCapacity: 12,
			ActiveFetchesKnown:     true,
		},
	})
	if !multiple.CrawlerFetchVisible || !multiple.CrawlerFetchAvailable ||
		multiple.CrawlerFetchValue != 7 || multiple.CrawlerFetchMaximum != 12 ||
		multiple.CrawlerFetchText != "7 active of 12 · 3 crawlers × 4 each" {
		t.Fatalf("multiple crawler reading = %+v", multiple)
	}
}

func TestCrawlerSystemMonitorRejectsUnknownAndInconsistentStates(t *testing.T) {
	t.Parallel()

	if view := buildSystemMonitorWithCrawler(nil, fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers:    -1,
			FetchLimitPerCrawler: 4,
			ActiveFetchesKnown:   true,
		},
	}); view.CrawlerFetchVisible {
		t.Fatalf("negative crawler count became visible: %+v", view)
	}

	invalid := []CrawlerFetchActivity{
		{ConnectedCrawlers: 1, FetchLimitPerCrawler: 4},
		{ConnectedCrawlers: 1, FetchLimitPerCrawler: 0, ActiveFetchesKnown: true},
		{
			ConnectedCrawlers:      2,
			FetchLimitPerCrawler:   4,
			AggregateFetchCapacity: 7,
			ActiveFetchesKnown:     true,
		},
		{
			ConnectedCrawlers:      1,
			ActiveFetches:          -1,
			FetchLimitPerCrawler:   4,
			AggregateFetchCapacity: 4,
			ActiveFetchesKnown:     true,
		},
	}
	for _, activity := range invalid {
		view := buildSystemMonitorWithCrawler(nil, fixedCrawlerFetchActivity{activity: activity})
		if !view.CrawlerFetchVisible || view.CrawlerFetchAvailable {
			t.Fatalf("invalid crawler reading became available: %+v", view)
		}
	}
}

func TestCrawlerSystemMonitorRendersEnabledAndDisabledStates(t *testing.T) {
	t.Parallel()

	enabled := do(t, New(Options{
		CrawlerFetchActivity: fixedCrawlerFetchActivity{
			activity: CrawlerFetchActivity{
				ConnectedCrawlers:      2,
				ActiveFetches:          5,
				FetchLimitPerCrawler:   4,
				AggregateFetchCapacity: 8,
				ActiveFetchesKnown:     true,
			},
		},
	}), systemMonitorPath)
	for _, want := range []string{
		">Crawler fetch workers<",
		"5 active of 8 · 2 crawlers × 4 each",
		`max="8" value="5" aria-label="Active crawler fetch workers"`,
	} {
		if !strings.Contains(enabled.body, want) {
			t.Fatalf("enabled crawler monitor missing %q: %s", want, enabled.body)
		}
	}

	disabled := do(t, New(Options{}), systemMonitorPath)
	if strings.Contains(disabled.body, "Crawler fetch workers") ||
		strings.Contains(disabled.body, "Active crawler fetch workers") {
		t.Fatalf("disabled crawler monitor exposed a row: %s", disabled.body)
	}

	unknown := do(t, New(Options{
		CrawlerFetchActivity: fixedCrawlerFetchActivity{
			activity: CrawlerFetchActivity{
				ConnectedCrawlers:      1,
				FetchLimitPerCrawler:   4,
				AggregateFetchCapacity: 4,
			},
		},
	}), systemMonitorPath)
	if !strings.Contains(unknown.body, ">Crawler fetch workers<") ||
		!strings.Contains(unknown.body, ">Unavailable<") ||
		strings.Contains(unknown.body, `aria-label="Active crawler fetch workers"`) {
		t.Fatalf("unknown crawler monitor state = %s", unknown.body)
	}
}
