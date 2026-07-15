package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/transfertally"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

type recordingDistributionObserver struct {
	receipts []dhtexchange.DistributionReceipt
}

func (o *recordingDistributionObserver) Observe(receipt dhtexchange.DistributionReceipt) {
	o.receipts = append(o.receipts, receipt)
}

func openTestTransferTally(t *testing.T) *transfertally.Tally {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	tally, err := transfertally.Open(v)
	if err != nil {
		t.Fatalf("transfertally.Open: %v", err)
	}

	return tally
}

func TestTallyOutboundObserverCountsSentTransfers(t *testing.T) {
	tally := openTestTransferTally(t)
	next := &recordingDistributionObserver{}
	observer := tallyOutboundObserver{next: next, tally: tally}

	observer.Observe(dhtexchange.DistributionReceipt{
		State:        dhtexchange.DistributionSent,
		PostingCount: 12,
		Handoff:      indextransfer.HandoffReceipt{SentURLRows: 4},
	})
	observer.Observe(dhtexchange.DistributionReceipt{
		State:        dhtexchange.DistributionHandoffFailed,
		PostingCount: 99,
	})

	totals, err := tally.Totals(context.Background())
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}
	if totals.SentWords != 12 || totals.SentURLs != 4 {
		t.Fatalf("totals = %+v, want sent words 12 and sent urls 4", totals)
	}
	if len(next.receipts) != 2 {
		t.Fatalf("forwarded receipts = %d, want 2", len(next.receipts))
	}
}

func TestTallyOutboundObserverWorksWithoutNext(t *testing.T) {
	observer := tallyOutboundObserver{tally: openTestTransferTally(t)}

	observer.Observe(dhtexchange.DistributionReceipt{
		State:        dhtexchange.DistributionSent,
		PostingCount: 1,
	})
}

func TestInboundReceiversCountReceivedTransfers(t *testing.T) {
	ctx := context.Background()
	tally := openTestTransferTally(t)

	postings := dhtInboundPostingReceiver{
		next:  &inboundPostingReceiverScript{receipt: rwi.Receipt{}},
		tally: tally,
		now:   time.Now,
	}
	if _, err := postings.Receive(ctx, []yagomodel.RWIPosting{
		{WordHash: yagomodel.WordHash("w1")},
		{WordHash: yagomodel.WordHash("w1")},
	}); err != nil {
		t.Fatalf("Receive postings: %v", err)
	}

	urls := dhtInboundURLReceiver{
		next:  &inboundURLReceiverScript{receipt: urlmeta.Receipt{}},
		tally: tally,
	}
	if _, err := urls.Receive(ctx, []yagomodel.URIMetadataRow{
		inboundMetadataRow(yagomodel.WordHash("u1")),
	}); err != nil {
		t.Fatalf("Receive urls: %v", err)
	}

	totals, err := tally.Totals(ctx)
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}
	if totals.ReceivedWords != 2 || totals.ReceivedURLs != 1 {
		t.Fatalf("totals = %+v, want received words 2 and received urls 1", totals)
	}
}

func TestTallyTransferReportsStorageFailure(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	tally, err := transfertally.Open(v)
	if err != nil {
		t.Fatalf("transfertally.Open: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	tallyTransfer(context.Background(), tally.AddSentWords, 1)
}

func TestReportedTransferTallyMapsTotals(t *testing.T) {
	ctx := context.Background()
	tally := openTestTransferTally(t)
	if err := tally.AddSentWords(ctx, 7); err != nil {
		t.Fatalf("AddSentWords: %v", err)
	}

	totals := reportedTransferTally{tally: tally}.TransferTotals(ctx)

	if totals.SentWords != 7 || !totals.Known {
		t.Fatalf("totals = %+v, want sent words 7 and known", totals)
	}
}
