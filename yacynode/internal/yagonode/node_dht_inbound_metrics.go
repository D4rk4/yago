package yagonode

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/metrics"
	"github.com/D4rk4/yago/yacynode/internal/rwi"
	"github.com/D4rk4/yago/yacynode/internal/urlmeta"
)

type inboundURLReferenceMatcher interface {
	ReferencedURLs(ctx context.Context, urls []yacymodel.Hash) ([]yacymodel.Hash, error)
}

type inboundURLMissingChecker interface {
	MissingURLs(ctx context.Context, urls []yacymodel.Hash) ([]yacymodel.Hash, error)
}

func observeDHTInboundStorage(
	storage nodeStorage,
	observer *metrics.DHTInboundMetrics,
) nodeStorage {
	if observer == nil {
		return storage
	}
	storage.postingReceiver = dhtInboundPostingReceiver{
		next:     storage.postingReceiver,
		observer: observer,
		now:      time.Now,
	}
	storage.urlReceiver = dhtInboundURLReceiver{
		next:       storage.urlReceiver,
		missing:    storage.urlDirectory,
		references: storage.references,
		observer:   observer,
	}

	return storage
}

type dhtInboundPostingReceiver struct {
	next     rwi.PostingReceiver
	observer *metrics.DHTInboundMetrics
	now      func() time.Time
}

func (r dhtInboundPostingReceiver) Receive(
	ctx context.Context,
	entries []yacymodel.RWIPosting,
) (rwi.Receipt, error) {
	started := r.now()
	receipt, err := r.next.Receive(ctx, entries)
	result := metrics.DHTInboundRWIResult{Duration: r.now().Sub(started)}
	if err != nil || receipt.Busy {
		result.RejectedPostings = len(entries)
		r.observer.ObserveRWI(result)
		if err != nil {
			return receipt, fmt.Errorf("receive rwi: %w", err)
		}

		return receipt, nil
	}
	result.ReceivedPostings = len(entries)
	result.UnknownURLs = len(receipt.UnknownURL)
	r.observer.ObserveRWI(result)

	return receipt, nil
}

type dhtInboundURLReceiver struct {
	next       urlmeta.URLReceiver
	missing    inboundURLMissingChecker
	references inboundURLReferenceMatcher
	observer   *metrics.DHTInboundMetrics
}

func (r dhtInboundURLReceiver) Receive(
	ctx context.Context,
	rows []yacymodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	hashes, invalid := urlRowHashes(rows)
	reconcileCandidates := r.reconcileCandidates(ctx, hashes)
	receipt, err := r.next.Receive(ctx, rows)
	if err != nil || receipt.Busy {
		r.observer.ObserveURL(metrics.DHTInboundURLResult{RejectedRows: len(rows)})
		if err != nil {
			return receipt, fmt.Errorf("receive url metadata: %w", err)
		}

		return receipt, nil
	}

	rejected := invalid + len(receipt.ErrorURL)
	r.observer.ObserveURL(metrics.DHTInboundURLResult{
		ReceivedRows:   len(rows) - rejected,
		RejectedRows:   rejected,
		ReconciledRows: reconciledURLRows(reconcileCandidates, receipt.ErrorURL),
	})

	return receipt, nil
}

func (r dhtInboundURLReceiver) reconcileCandidates(
	ctx context.Context,
	hashes []yacymodel.Hash,
) []yacymodel.Hash {
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

func urlRowHashes(rows []yacymodel.URIMetadataRow) ([]yacymodel.Hash, int) {
	hashes := make([]yacymodel.Hash, 0, len(rows))
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

func reconciledURLRows(candidates, rejected []yacymodel.Hash) int {
	rejectedSet := make(map[yacymodel.Hash]struct{}, len(rejected))
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
