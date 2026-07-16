package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

func TestSearchIndexWriteMetricsSeparateSuccessAndFailure(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	writes := NewSearchIndexWriteMetrics(registry)
	writes.ObserveSearchIndexWrite(250*time.Millisecond, 3, false)
	writes.ObserveSearchIndexWrite(500*time.Millisecond, 11, true)

	if got := testutil.ToFloat64(writes.documents); got != 3 {
		t.Fatalf("indexed documents = %v, want 3", got)
	}
	if got := testutil.ToFloat64(writes.failures); got != 1 {
		t.Fatalf("write failures = %v, want 1", got)
	}
	histogram := &dto.Metric{}
	if err := writes.duration.Write(histogram); err != nil {
		t.Fatalf("write duration metric: %v", err)
	}
	if histogram.GetHistogram().GetSampleCount() != 2 ||
		histogram.GetHistogram().GetSampleSum() != 0.75 {
		t.Fatalf("write duration = %v", histogram.GetHistogram())
	}
	metricNames := []string{
		"crawl_search_index_write_duration_seconds",
		"crawl_search_index_documents_total",
		"crawl_search_index_write_failures_total",
	}
	if got, err := testutil.GatherAndCount(registry, metricNames...); err != nil || got != 3 {
		t.Fatalf("gathered metric families = %d, %v", got, err)
	}
}
