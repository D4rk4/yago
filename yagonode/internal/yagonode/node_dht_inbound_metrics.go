package yagonode

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/transfertally"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

func observeDHTInboundStorage(
	storage nodeStorage,
	observer *metrics.DHTInboundMetrics,
	tally *transfertally.Tally,
) nodeStorage {
	if observer == nil && tally == nil {
		return storage
	}
	reconciliation := newDHTInboundReconciliation(maximumPendingDHTInboundURLs)
	storage.postingReceiver = dhtInboundPostingReceiver{
		next:           storage.postingReceiver,
		observer:       observer,
		tally:          tally,
		reconciliation: reconciliation,
		now:            time.Now,
	}
	storage.urlReceiver = dhtInboundURLReceiver{
		next:           storage.urlReceiver,
		observer:       observer,
		tally:          tally,
		reconciliation: reconciliation,
	}

	return storage
}

type dhtInboundPostingReceiver struct {
	next           rwi.PostingReceiver
	observer       *metrics.DHTInboundMetrics
	tally          *transfertally.Tally
	reconciliation *dhtInboundReconciliation
	now            func() time.Time
}

func (r dhtInboundPostingReceiver) Receive(
	ctx context.Context,
	entries []yagomodel.RWIPosting,
) (rwi.Receipt, error) {
	started := r.now()
	receipt, err := r.next.Receive(ctx, entries)
	result := metrics.DHTInboundRWIResult{Duration: r.now().Sub(started)}
	if err != nil || receipt.Busy {
		result.RejectedPostings = len(entries)
		r.observeRWI(result)
		if err != nil {
			return receipt, fmt.Errorf("receive rwi: %w", err)
		}

		return receipt, nil
	}
	result.ReceivedPostings = len(entries)
	result.UnknownURLs = len(receipt.UnknownURL)
	r.reconciliation.note(receipt.UnknownURL)
	r.observeRWI(result)
	if r.tally != nil {
		tallyTransfer(ctx, r.tally.AddReceivedWords, result.ReceivedPostings)
	}

	return receipt, nil
}

func (r dhtInboundPostingReceiver) observeRWI(result metrics.DHTInboundRWIResult) {
	if r.observer != nil {
		r.observer.ObserveRWI(result)
	}
}

type dhtInboundURLReceiver struct {
	next           urlmeta.URLReceiver
	observer       *metrics.DHTInboundMetrics
	tally          *transfertally.Tally
	reconciliation *dhtInboundReconciliation
}

func (r dhtInboundURLReceiver) Receive(
	ctx context.Context,
	rows []yagomodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	invalid := invalidURLRowCount(rows)
	receipt, err := r.next.Receive(ctx, rows)
	if err != nil || receipt.Busy {
		r.observeURL(metrics.DHTInboundURLResult{RejectedRows: len(rows)})
		if err != nil {
			return receipt, fmt.Errorf("receive url metadata: %w", err)
		}

		return receipt, nil
	}

	rejected := invalid + len(receipt.ErrorURL)
	received := len(rows) - rejected
	reconciled := r.reconciliation.resolve(rows, receipt.ErrorURL, receipt.ExistingURL)
	r.observeURL(metrics.DHTInboundURLResult{
		ReceivedRows:   received,
		RejectedRows:   rejected,
		ReconciledRows: reconciled,
	})
	if r.tally != nil {
		tallyTransfer(ctx, r.tally.AddReceivedURLs, received)
	}

	return receipt, nil
}

func (r dhtInboundURLReceiver) observeURL(result metrics.DHTInboundURLResult) {
	if r.observer != nil {
		r.observer.ObserveURL(result)
	}
}

func invalidURLRowCount(rows []yagomodel.URIMetadataRow) int {
	var invalid int
	for _, row := range rows {
		if _, err := row.URLHash(); err != nil {
			invalid++
		}
	}

	return invalid
}
