package eviction

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/urlmetastaleness"
	"github.com/D4rk4/yago/yagonode/internal/urlreferences"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type quotaSweeper struct {
	vault       *vault.Vault
	postings    rwi.PostingPurger
	references  urlreferences.ReferenceQuery
	urls        urlmeta.URLEvictor
	documents   DocumentEvictor
	resolver    URLResolver
	stale       urlmetastaleness.StaleURLSource
	postingOnly PostingOnlyURLSource
	target      float64
	batch       int
}

func (s quotaSweeper) Sweep(ctx context.Context) (Result, error) {
	quota := s.vault.QuotaBytes()
	if quota <= 0 || s.batch <= 0 {
		return Result{}, nil
	}
	highWater := int64(float64(quota) * s.target)

	var total Result
	for {
		used, err := s.vault.UsedBytes(ctx)
		if err != nil {
			return total, fmt.Errorf("measure usage: %w", err)
		}
		if used < highWater {
			return total, nil
		}

		batchSelection, err := s.nextEvictionBatch(ctx)
		if err != nil {
			return total, err
		}
		if len(batchSelection.urls) == 0 {
			return total, nil
		}

		batch, err := s.purge(ctx, batchSelection.urls)
		if err != nil {
			return total, err
		}
		total.URLsDeleted += batch.URLsDeleted
		total.PostingsDeleted += batch.PostingsDeleted
		total.DocumentsDeleted += batch.DocumentsDeleted
		if batchSelection.stalled(batch) {
			return total, nil
		}
	}
}

type evictionBatchSelection struct {
	urls        []yagomodel.Hash
	postingOnly bool
}

func (selection evictionBatchSelection) stalled(result Result) bool {
	if selection.postingOnly {
		return result.PostingsDeleted == 0
	}

	return result.URLsDeleted == 0
}

func (s quotaSweeper) nextEvictionBatch(ctx context.Context) (evictionBatchSelection, error) {
	candidates, err := s.stale.StalestURLs(ctx, s.batch)
	if err != nil {
		return evictionBatchSelection{}, fmt.Errorf("select stale urls: %w", err)
	}
	if len(candidates) > 0 || s.postingOnly == nil {
		return evictionBatchSelection{urls: candidates}, nil
	}

	candidates, err = s.postingOnly.PostingOnlyURLs(ctx, s.batch)
	if err != nil {
		return evictionBatchSelection{}, fmt.Errorf("select posting-only urls: %w", err)
	}

	return evictionBatchSelection{urls: candidates, postingOnly: len(candidates) > 0}, nil
}

func (s quotaSweeper) purge(ctx context.Context, urls []yagomodel.Hash) (Result, error) {
	return purgeURLs(ctx, s.vault, s.postings, s.references, s.urls, s.documents, s.resolver, urls)
}

//nolint:revive // each argument is a distinct collection the purge touches; bundling them would invent a hollow type
func purgeURLs(
	ctx context.Context,
	v *vault.Vault,
	postings rwi.PostingPurger,
	references urlreferences.ReferenceQuery,
	evictor urlmeta.URLEvictor,
	documents DocumentEvictor,
	resolver URLResolver,
	urls []yagomodel.Hash,
) (Result, error) {
	normalizedURLs, err := resolveDocumentURLs(ctx, documents, resolver, urls)
	if err != nil {
		return Result{}, err
	}

	deletion := resolvedURLLineageDeletion{
		vault:     v,
		documents: documents,
		projections: urlProjectionDeletion{
			postings:   postings,
			references: references,
			metadata:   evictor,
		},
	}

	return deletion.purge(
		ctx,
		normalizedURLs,
		urls,
	)
}

type resolvedURLLineageDeletion struct {
	vault       *vault.Vault
	documents   DocumentEvictor
	projections urlProjectionDeletion
}

func (d resolvedURLLineageDeletion) purge(
	ctx context.Context,
	normalizedURLs []string,
	urls []yagomodel.Hash,
) (Result, error) {
	normalizedURLs = canonicalPurgeDocumentURLs(normalizedURLs)
	reservation, err := reserveDocumentEvictions(ctx, d.documents, normalizedURLs)
	if err != nil {
		return Result{}, err
	}
	if reservation != nil {
		defer reservation.Release()
	}
	documentsDeleted, err := purgeReservedDocuments(ctx, reservation, normalizedURLs)
	if err != nil {
		return Result{}, err
	}

	result := Result{DocumentsDeleted: documentsDeleted}
	var urlResult urlmeta.PurgeResult
	err = d.vault.Update(ctx, func(tx *vault.Txn) error {
		projected, purged, err := d.projections.delete(ctx, tx, urls)
		if err != nil {
			return err
		}
		projected.DocumentsDeleted = documentsDeleted
		result = projected
		urlResult = purged

		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("purge batch: %w", err)
	}
	urlResult.ReportObserverFailures(ctx)

	return result, nil
}

func canonicalPurgeDocumentURLs(normalizedURLs []string) []string {
	seen := make(map[string]struct{}, len(normalizedURLs))
	canonical := make([]string, 0, len(normalizedURLs))
	for _, rawURL := range normalizedURLs {
		normalizedURL := strings.TrimSpace(rawURL)
		if normalizedURL == "" {
			continue
		}
		if _, duplicate := seen[normalizedURL]; duplicate {
			continue
		}
		seen[normalizedURL] = struct{}{}
		canonical = append(canonical, normalizedURL)
	}
	sort.Strings(canonical)

	return canonical
}

func resolveDocumentURLs(
	ctx context.Context,
	documents DocumentEvictor,
	resolver URLResolver,
	urls []yagomodel.Hash,
) ([]string, error) {
	if documents == nil || resolver == nil || len(urls) == 0 {
		return nil, nil
	}

	rows, err := resolver.RowsByHash(ctx, urls)
	if err != nil {
		return nil, fmt.Errorf("resolve urls for document purge: %w", err)
	}

	normalizedURLs := make([]string, 0, len(rows))
	for _, row := range rows {
		url, err := yagomodel.DecodeWireForm(ctx, row.Properties[yagomodel.URLMetaURL])
		if err != nil || url == "" {
			continue
		}
		normalizedURLs = append(normalizedURLs, url)
	}

	return normalizedURLs, nil
}

func reserveDocumentEvictions(
	ctx context.Context,
	documents DocumentEvictor,
	normalizedURLs []string,
) (ReservedDocumentEviction, error) {
	if documents == nil || len(normalizedURLs) == 0 {
		return nil, nil
	}
	if reserver, ok := documents.(documentEvictionReserver); ok {
		reservation, err := reserver.ReserveDocumentEvictions(ctx, normalizedURLs)
		if err != nil {
			return nil, fmt.Errorf("reserve document evictions: %w", err)
		}
		if reservation == nil {
			return nil, fmt.Errorf("reserve document evictions: empty reservation")
		}

		return reservation, nil
	}

	return directDocumentEviction{documents: documents}, nil
}

type directDocumentEviction struct {
	documents DocumentEvictor
}

func (d directDocumentEviction) Delete(
	ctx context.Context,
	normalizedURL string,
) (bool, error) {
	removed, err := d.documents.Delete(ctx, normalizedURL)
	if err != nil {
		return false, fmt.Errorf("delete direct document eviction: %w", err)
	}

	return removed, nil
}

func (directDocumentEviction) Release() {}

func purgeReservedDocuments(
	ctx context.Context,
	reservation ReservedDocumentEviction,
	normalizedURLs []string,
) (int, error) {
	if reservation == nil {
		return 0, nil
	}
	deleted := 0
	for _, normalizedURL := range normalizedURLs {
		removed, err := reservation.Delete(ctx, normalizedURL)
		if err != nil {
			return 0, fmt.Errorf("delete document: %w", err)
		}
		if removed {
			deleted++
		}
	}

	return deleted, nil
}
