package rwi

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

// TestSelectOutboundStopsAtPostingBudgetWithinOneWord pins the outbound posting
// budget where the word budget cannot stand in for it: three postings of a
// single word under a two-posting budget must hand exactly two to the transfer
// and leave the third in the live index for the next round. Every existing limit
// test caps words at the same moment, so an off-by-one posting budget would ship
// one row more than the transfer was sized for and still pass.
func TestSelectOutboundStopsAtPostingBudgetWithinOneWord(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	word := yagomodel.Hash("AAAAAAAAAAAA")
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{
		postingWithHashes(word, yagomodel.Hash("CCCCCCCCCCCC")),
		postingWithHashes(word, yagomodel.Hash("DDDDDDDDDDDD")),
		postingWithHashes(word, yagomodel.Hash("EEEEEEEEEEEE")),
	}); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	selection, err := outboundStore(t, h.rwi.Index).SelectOutbound(
		ctx,
		OutboundSelectionConfig{MaxWords: 4, MaxPostings: 2},
	)
	if err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}
	if selection.PostingCount() != 2 {
		t.Fatalf("selection = %#v, want exactly the two budgeted postings", selection)
	}
	count, err := h.rwi.Index.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if count != 1 {
		t.Fatalf("RWICount = %d, want the unbudgeted posting retained", count)
	}
}
