package metrics

import (
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestEndpointCountsRequestsByEndpointAndStatus(t *testing.T) {
	endpoints := NewHTTPEndpointMetrics()

	endpoints.Observe("/yacy/transferRWI.html", http.StatusOK, 2*time.Millisecond)
	endpoints.Observe("/yacy/transferRWI.html", http.StatusOK, 4*time.Millisecond)
	endpoints.Observe("/yacy/transferRWI.html", http.StatusBadRequest, time.Millisecond)
	endpoints.Observe("", http.StatusNotFound, time.Millisecond)

	served := endpoints.requests
	if got := testutil.ToFloat64(
		served.WithLabelValues("/yacy/transferRWI.html", "200"),
	); got != 2 {
		t.Errorf("ok requests = %v, want 2", got)
	}
	if got := testutil.ToFloat64(
		served.WithLabelValues("/yacy/transferRWI.html", "400"),
	); got != 1 {
		t.Errorf("bad requests = %v, want 1", got)
	}
	if got := testutil.ToFloat64(served.WithLabelValues(unmatchedEndpoint, "404")); got != 1 {
		t.Errorf("unmatched requests = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(endpoints.durations); got != 2 {
		t.Errorf("timed endpoints = %v, want 2", got)
	}
}

func TestEndpointRegistryGathersObservations(t *testing.T) {
	endpoints := NewHTTPEndpointMetrics()
	endpoints.Observe("/yacy/transferRWI.html", http.StatusOK, time.Millisecond)

	got, err := testutil.GatherAndCount(endpoints.Registry(), "http_requests_total")
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if got != 1 {
		t.Errorf("request series = %v, want 1", got)
	}
}
