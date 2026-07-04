package yagonode

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/transfertally"
)

const (
	msgTransferTotalsUnavailable = "transfer totals unavailable for self seed"
	msgTransferTallyFailed       = "transfer tally update failed"
)

type reportedTransferTally struct {
	tally *transfertally.Tally
}

func (r reportedTransferTally) TransferTotals(ctx context.Context) nodestatus.TransferTotals {
	totals, err := r.tally.Totals(ctx)
	if err != nil {
		slog.WarnContext(ctx, msgTransferTotalsUnavailable, slog.Any("error", err))

		return nodestatus.TransferTotals{}
	}

	return nodestatus.TransferTotals{
		SentWords:     totals.SentWords,
		ReceivedWords: totals.ReceivedWords,
		SentURLs:      totals.SentURLs,
		ReceivedURLs:  totals.ReceivedURLs,
	}
}

type tallyOutboundObserver struct {
	next  dhtexchange.DistributionObserver
	tally *transfertally.Tally
}

func (o tallyOutboundObserver) Observe(receipt dhtexchange.DistributionReceipt) {
	if o.next != nil {
		o.next.Observe(receipt)
	}
	if receipt.State != dhtexchange.DistributionSent {
		return
	}
	ctx := context.Background()
	tallyTransfer(ctx, o.tally.AddSentWords, receipt.PostingCount)
	tallyTransfer(ctx, o.tally.AddSentURLs, receipt.Handoff.SentURLRows)
}

func tallyTransfer(ctx context.Context, add func(context.Context, int) error, n int) {
	if err := add(ctx, n); err != nil {
		slog.WarnContext(ctx, msgTransferTallyFailed, slog.Any("error", err))
	}
}
