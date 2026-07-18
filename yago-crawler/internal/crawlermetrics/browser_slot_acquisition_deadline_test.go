package crawlermetrics_test

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
)

func TestMetricsExposeBrowserSlotAcquisitionDeadlinesWithoutLabels(t *testing.T) {
	metrics := crawlermetrics.New()
	metrics.ObserveBrowserSlotAcquisitionDeadline()
	metrics.ObserveBrowserSlotAcquisitionDeadline()
	body := scrapeMetrics(t, metrics)
	const metric = "yacy_crawler_browser_slot_acquisition_deadlines_total"
	if !strings.Contains(body, "\n"+metric+" 2\n") {
		t.Fatalf("metrics missing browser slot acquisition deadline count:\n%s", body)
	}
	if strings.Contains(body, metric+"{") {
		t.Fatalf("browser slot acquisition deadline metric has labels:\n%s", body)
	}
}
