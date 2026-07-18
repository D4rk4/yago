package crawlorder

import "testing"

func TestActiveOrdersDeduplicateEmptyProvenanceAcrossLeases(t *testing.T) {
	active := newActiveOrders()
	for _, leaseID := range []string{"lease-a", "lease-b"} {
		delivery := CrawlOrderDelivery{LeaseID: leaseID}
		if claim := active.claim(nil, delivery); claim != activeOrderStartsRun {
			t.Fatalf("claim for %q = %d, want start", leaseID, claim)
		}
	}

	provenances := active.provenances()
	if len(provenances) != 1 || len(provenances[0]) != 0 {
		t.Fatalf("provenances = %q, want one empty provenance", provenances)
	}
}
