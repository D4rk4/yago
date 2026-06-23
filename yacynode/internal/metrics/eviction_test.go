package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/eviction"
)

func TestEvictionCountsSweptWork(t *testing.T) {
	observer := NewEviction(prometheus.NewRegistry())

	observer.Observe(eviction.Result{URLsDeleted: 3, PostingsDeleted: 7})
	observer.Observe(eviction.Result{URLsDeleted: 2, PostingsDeleted: 1})

	if got := testutil.ToFloat64(observer.urls); got != 5 {
		t.Fatalf("urls evicted = %v, want 5", got)
	}
	if got := testutil.ToFloat64(observer.postings); got != 8 {
		t.Fatalf("postings evicted = %v, want 8", got)
	}
}

func TestEvictionCountsFailures(t *testing.T) {
	observer := NewEviction(prometheus.NewRegistry())

	observer.ObserveFailure()
	observer.ObserveFailure()

	if got := testutil.ToFloat64(observer.failures); got != 2 {
		t.Fatalf("failures = %v, want 2", got)
	}
}
