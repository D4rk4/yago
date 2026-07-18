package yagonode

import (
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestDHTInboundReconciliationEvictsOldestUnknownURLAtCapacity(t *testing.T) {
	first := yagomodel.Hash("FirstURL0001")
	second := yagomodel.Hash("SecondURL001")
	tracker := newDHTInboundReconciliation(1)
	tracker.note([]yagomodel.Hash{first, first, second})

	if got := tracker.resolve([]yagomodel.URIMetadataRow{
		inboundMetadataRow(first),
		inboundMetadataRow(second),
	}, nil, nil); got != 1 {
		t.Fatalf("reconciled = %d, want one", got)
	}
	if got := tracker.resolve(
		[]yagomodel.URIMetadataRow{inboundMetadataRow(second)},
		nil,
		nil,
	); got != 0 {
		t.Fatalf("second reconciliation = %d, want zero", got)
	}
}

func TestDHTInboundReconciliationDisablesTrackingAtZeroCapacity(t *testing.T) {
	tracker := newDHTInboundReconciliation(0)
	tracker.note([]yagomodel.Hash{"PendingURL01"})
	if got := tracker.resolve([]yagomodel.URIMetadataRow{
		inboundMetadataRow("PendingURL01"),
	}, nil, nil); got != 0 {
		t.Fatalf("reconciled = %d, want zero", got)
	}
}

func TestDHTInboundReconciliationCompactsResolvedArrivals(t *testing.T) {
	tracker := newDHTInboundReconciliation(2)
	for iteration := range 20 {
		hash := yagomodel.Hash(fmt.Sprintf("URLHash%05d", iteration))
		tracker.note([]yagomodel.Hash{hash})
		if tracker.resolve(
			[]yagomodel.URIMetadataRow{inboundMetadataRow(hash)},
			nil,
			nil,
		) != 1 {
			t.Fatalf("iteration %d was not reconciled", iteration)
		}
	}
	if len(tracker.arrival) > 2*tracker.capacity {
		t.Fatalf("arrival retention = %d, capacity = %d", len(tracker.arrival), tracker.capacity)
	}
}

func TestDHTInboundReconciliationRetainsRejectedURLs(t *testing.T) {
	hash := yagomodel.Hash("PendingURL01")
	tracker := newDHTInboundReconciliation(1)
	tracker.note([]yagomodel.Hash{hash})

	if got := tracker.resolve(
		[]yagomodel.URIMetadataRow{{}, inboundMetadataRow(hash)},
		[]yagomodel.Hash{hash},
		nil,
	); got != 0 {
		t.Fatalf("rejected reconciliation = %d", got)
	}
	if got := tracker.resolve(
		[]yagomodel.URIMetadataRow{inboundMetadataRow(hash)},
		nil,
		nil,
	); got != 1 {
		t.Fatalf("retried reconciliation = %d, want one", got)
	}
}

func TestDHTInboundReconciliationCountsStoredRowAlongsideDuplicateIdentity(t *testing.T) {
	hash := yagomodel.Hash("PendingURL01")
	tracker := newDHTInboundReconciliation(1)
	tracker.note([]yagomodel.Hash{hash})

	if got := tracker.resolve(
		[]yagomodel.URIMetadataRow{inboundMetadataRow(hash), inboundMetadataRow(hash)},
		nil,
		[]yagomodel.Hash{hash},
	); got != 1 {
		t.Fatalf("reconciled = %d, want one stored identity", got)
	}
	if got := tracker.resolve(
		[]yagomodel.URIMetadataRow{inboundMetadataRow(hash)},
		nil,
		nil,
	); got != 0 {
		t.Fatalf("second reconciliation = %d, want released identity", got)
	}
}

func TestDHTInboundReconciliationHandlesAbsentTracker(t *testing.T) {
	var tracker *dhtInboundReconciliation
	tracker.note(nil)
	if tracker.resolve(nil, nil, nil) != 0 {
		t.Fatal("absent tracker reconciled rows")
	}
}
