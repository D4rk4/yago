package visitmetrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacyvisitcrawl/internal/visitmetrics"
)

func TestMetricsRecordAndExpose(t *testing.T) {
	metrics := visitmetrics.New()
	metrics.VisitReceived()
	metrics.VisitRejected()
	metrics.OrderPlaced()
	metrics.OrderUnplaced()

	recorder := httptest.NewRecorder()
	metrics.Handler().
		ServeHTTP(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil))

	body := recorder.Body.String()
	for _, want := range []string{
		"yacyvisitcrawl_visits_received_total 1",
		"yacyvisitcrawl_visits_rejected_total 1",
		"yacyvisitcrawl_orders_placed_total 1",
		"yacyvisitcrawl_orders_unplaced_total 1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}
