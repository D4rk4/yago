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

type inboundURLReferenceMatcher interface {
	ReferencedURLs(ctx context.Context, urls []yagomodel.Hash) ([]yagomodel.Hash, error)
}

type inboundURLMissingChecker interface {
	MissingURLs(ctx context.Context, urls []yagomodel.Hash) ([]yagomodel.Hash, error)
}

func observeDHTInboundStorage(
	storage nodeStorage,
	observer *metrics.DHTInboundMetrics,
	tally *transfertally.Tally,
) nodeStorage {
	if observer == nil && tally == nil {
		return storage
	}
	storage.postingReceiver = dhtInboundPostingReceiver{
		next:     storage.postingReceiver,
		observer: observer,
		tally:    tally,
		now:      time.Now,
	}
	storage.urlReceiver = dhtInboundURLReceiver{
		next:       storage.urlReceiver,
		missing:    storage.urlDirectory,
		references: storage.references,
		observer:   observer,
		tally:      tally,
	}

	return storage
}

type dhtInboundPostingReceiver struct {
	next     rwi.PostingReceiver
	observer *metrics.DHTInboundMetrics
	tally    *transfertally.Tally
	now      func() time.Time
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
	next       urlmeta.URLReceiver
	missing    inboundURLMissingChecker
	references inboundURLReferenceMatcher
	observer   *metrics.DHTInboundMetrics
	tally      *transfertally.Tally
}

func (r dhtInboundURLReceiver) Receive(
	ctx context.Context,
	rows []yagomodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	hashes, invalid := urlRowHashes(rows)
	reconcileCandidates := r.reconcileCandidates(ctx, hashes)
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
	r.observeURL(metrics.DHTInboundURLResult{
		ReceivedRows:   received,
		RejectedRows:   rejected,
		ReconciledRows: reconciledURLRows(reconcileCandidates, receipt.ErrorURL),
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

func (r dhtInboundURLReceiver) reconcileCandidates(
	ctx context.Context,
	hashes []yagomodel.Hash,
) []yagomodel.Hash {
	if r.missing == nil || r.references == nil {
		return nil
	}
	missing, err := r.missing.MissingURLs(ctx, hashes)
	if err != nil {
		return nil
	}
	referenced, err := r.references.ReferencedURLs(ctx, missing)
	if err != nil {
		return nil
	}

	return referenced
}

func urlRowHashes(rows []yagomodel.URIMetadataRow) ([]yagomodel.Hash, int) {
	hashes := make([]yagomodel.Hash, 0, len(rows))
	var invalid int
	for _, row := range rows {
		hash, err := row.URLHash()
		if err != nil {
			invalid++

			continue
		}
		hashes = append(hashes, hash.Hash())
	}

	return hashes, invalid
}

func reconciledURLRows(candidates, rejected []yagomodel.Hash) int {
	rejectedSet := make(map[yagomodel.Hash]struct{}, len(rejected))
	for _, hash := range rejected {
		rejectedSet[hash] = struct{}{}
	}
	reconciled := 0
	for _, hash := range candidates {
		if _, ok := rejectedSet[hash]; ok {
			continue
		}
		reconciled++
	}

	return reconciled
}
