package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestDHTInboundMetricsCountsRWI(t *testing.T) {
	observer := NewDHTInboundMetrics(prometheus.NewRegistry())

	observer.ObserveRWI(DHTInboundRWIResult{
		ReceivedPostings: 3,
		RejectedPostings: 2,
		UnknownURLs:      1,
		Duration:         2 * time.Second,
	})

	if got := testutil.ToFloat64(observer.rwiReceived); got != 3 {
		t.Fatalf("rwi received = %v, want 3", got)
	}
	if got := testutil.ToFloat64(observer.rwiRejected); got != 2 {
		t.Fatalf("rwi rejected = %v, want 2", got)
	}
	if got := testutil.ToFloat64(observer.rwiUnknownURL); got != 1 {
		t.Fatalf("rwi unknown url = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(observer.rwiDuration); got == 0 {
		t.Fatal("rwi duration histogram was not collected")
	}
}

func TestDHTInboundMetricsCountsURL(t *testing.T) {
	observer := NewDHTInboundMetrics(prometheus.NewRegistry())

	observer.ObserveURL(DHTInboundURLResult{
		ReceivedRows:   5,
		RejectedRows:   2,
		ReconciledRows: 3,
	})

	if got := testutil.ToFloat64(observer.urlReceived); got != 5 {
		t.Fatalf("url received = %v, want 5", got)
	}
	if got := testutil.ToFloat64(observer.urlRejected); got != 2 {
		t.Fatalf("url rejected = %v, want 2", got)
	}
	if got := testutil.ToFloat64(observer.urlReconciled); got != 3 {
		t.Fatalf("url reconciled = %v, want 3", got)
	}
}
