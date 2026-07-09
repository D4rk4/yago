package indexmetrics

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIndexMetricsRecordsAndExposesCounters(t *testing.T) {
	metrics := New()

	metrics.PageReceived()
	metrics.PageIndexed()
	metrics.PageDisposed("undecodable")
	metrics.IndexFailed()
	metrics.IndexObserved(250 * time.Millisecond)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		"yacytextindexer_pages_received_total 1",
		"yacytextindexer_pages_indexed_total 1",
		`yacytextindexer_pages_disposed_total{reason="undecodable"} 1`,
		"yacytextindexer_index_failures_total 1",
		"yacytextindexer_index_duration_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics output missing %q, got:\n%s", want, body)
		}
	}
}
