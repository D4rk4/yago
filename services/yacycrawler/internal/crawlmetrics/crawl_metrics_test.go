package crawlmetrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlmetrics"
)

func TestMetricsRecordAndExpose(t *testing.T) {
	metrics := crawlmetrics.New()
	metrics.OrderReceived()
	metrics.OrderCompleted()
	metrics.OrderRedelivered()
	metrics.PageFetched()
	metrics.PagePublished("index")
	metrics.PageDisposed("unsupported-media-type")
	metrics.RefusalHonored("ceased")
	metrics.PublicationWaited()
	metrics.BudgetExhausted()
	metrics.FetchObserved(250 * time.Millisecond)

	recorder := httptest.NewRecorder()
	metrics.Handler().
		ServeHTTP(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil))

	body := recorder.Body.String()
	for _, want := range []string{
		"yacycrawler_orders_received_total 1",
		"yacycrawler_pages_published_total",
		"yacycrawler_pages_disposed_total",
		"yacycrawler_fetch_duration_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}
