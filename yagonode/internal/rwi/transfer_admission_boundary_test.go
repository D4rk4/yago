package rwi

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

// TestTransferRWIAdmitsBatchExactlyAtTransferCap pins the accepting side of the
// per-transfer batch bound: the cap is the largest batch a peer may hand over,
// not the first batch turned away. Only the refusing side is pinned today, so
// tightening the bound by one would still pass every existing test while costing
// each sending peer one entry per transfer and reporting busy for a batch the
// configuration allows.
func TestTransferRWIAdmitsBatchExactlyAtTransferCap(t *testing.T) {
	h := openHarness(t, 0, 2)

	response, err := h.endpoint().Serve(t.Context(), yagoproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		WordCount:   2,
		EntryCount:  2,
		Indexes: []yagomodel.RWIPosting{
			posting("w1", "u1"),
			posting("w2", "u2"),
		},
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if response.Result != yagoproto.ResultOK {
		t.Fatalf("Result = %q, want ok for a batch of exactly the cap", response.Result)
	}
	if response.Pause != 0 {
		t.Fatalf("Pause = %d, want no backoff for an admitted batch", response.Pause)
	}

	count, err := h.rwi.Index.RWICount(t.Context())
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if count != 2 {
		t.Fatalf("RWICount = %d, want both admitted entries stored", count)
	}
}
