package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/dhtexchange"
)

func TestDHTOutboundMetricsCountsSuccessfulTransfer(t *testing.T) {
	observer := NewDHTOutboundMetrics(prometheus.NewRegistry())
	receipt := dhtexchange.DistributionReceipt{
		State:        dhtexchange.DistributionSent,
		PostingCount: 7,
	}
	receipt.Handoff.RemoteUnknownURL = make([]yacymodel.Hash, 3)

	observer.Observe(receipt)

	if got := testutil.ToFloat64(observer.batches); got != 1 {
		t.Fatalf("batches = %v, want 1", got)
	}
	if got := testutil.ToFloat64(observer.postings); got != 7 {
		t.Fatalf("postings = %v, want 7", got)
	}
	if got := testutil.ToFloat64(observer.unknownURL); got != 3 {
		t.Fatalf("unknown url = %v, want 3", got)
	}
	if got := testutil.ToFloat64(observer.failures); got != 0 {
		t.Fatalf("failures = %v, want 0", got)
	}
}

func TestDHTOutboundMetricsCountsFailures(t *testing.T) {
	observer := NewDHTOutboundMetrics(prometheus.NewRegistry())

	observer.Observe(dhtexchange.DistributionReceipt{State: dhtexchange.DistributionCapacityFailed})
	observer.Observe(dhtexchange.DistributionReceipt{State: dhtexchange.DistributionHandoffFailed})
	observer.Observe(
		dhtexchange.DistributionReceipt{State: dhtexchange.DistributionHandoffRejected},
	)
	observer.Observe(dhtexchange.DistributionReceipt{State: dhtexchange.DistributionGateClosed})
	observer.Observe(dhtexchange.DistributionReceipt{State: dhtexchange.DistributionQueueEmpty})

	if got := testutil.ToFloat64(observer.failures); got != 3 {
		t.Fatalf("failures = %v, want 3", got)
	}
	if got := testutil.ToFloat64(observer.batches); got != 0 {
		t.Fatalf("batches = %v, want 0", got)
	}
	if got := testutil.ToFloat64(observer.postings); got != 0 {
		t.Fatalf("postings = %v, want 0", got)
	}
}
