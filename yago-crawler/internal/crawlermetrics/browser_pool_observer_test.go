package crawlermetrics_test

import (
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/browserpool"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
)

func TestMetricsExposeBoundedBrowserPoolObservations(t *testing.T) {
	metrics := crawlermetrics.New()
	metrics.ObserveBrowserSlotWait(250 * time.Millisecond)
	metrics.ObserveBrowserPoolState(browserpool.State{
		Ready:   2,
		Busy:    1,
		Cooling: 3,
	})
	for _, reason := range browserpool.FailureReasons() {
		metrics.ObserveBrowserFailure(reason)
	}
	metrics.ObserveBrowserFailure("unbounded-error-text")
	body := scrapeMetrics(t, metrics)
	for _, sample := range []string{
		"yacy_crawler_browser_slot_acquisition_seconds_count 1",
		`yacy_crawler_browser_sessions{state="ready"} 2`,
		`yacy_crawler_browser_sessions{state="busy"} 1`,
		`yacy_crawler_browser_sessions{state="cooling"} 3`,
		`yacy_crawler_browser_failures_total{reason="slot_deadline"} 1`,
		`yacy_crawler_browser_failures_total{reason="cooldown"} 1`,
		`yacy_crawler_browser_failures_total{reason="launch"} 1`,
		`yacy_crawler_browser_failures_total{reason="render"} 1`,
	} {
		if !strings.Contains(body, sample) {
			t.Fatalf("metric %q missing:\n%s", sample, body)
		}
	}
	if strings.Contains(body, "unbounded-error-text") {
		t.Fatalf("unexpected dynamic browser failure label:\n%s", body)
	}
}
