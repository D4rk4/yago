package documentstore

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type DocumentLineageReservation interface {
	documentLineageReservation()
}

type DocumentLineageReserver interface {
	ReserveDocumentLineages(context.Context, []string) (DocumentLineageReservation, error)
	ReleaseDocumentLineages(DocumentLineageReservation)
}

type ReservedOutboundAnchorReceiver interface {
	ReplaceReservedOutboundAnchors(
		context.Context,
		DocumentLineageReservation,
		[]OutboundAnchorSet,
	) (AnchorUpdate, error)
}

type ReservedCanonicalDocumentDirectory interface {
	CanonicalReservedDocuments(
		context.Context,
		DocumentLineageReservation,
		[]Document,
	) ([]Document, error)
}

type documentLineageLease struct {
	once       sync.Once
	released   atomic.Bool
	boundaries *storedDocumentURLBoundaries
	sources    map[string]struct{}
	release    func()
}

func (*documentLineageLease) documentLineageReservation() {}

func (d documentVault) ReserveDocumentLineages(
	ctx context.Context,
	urls []string,
) (DocumentLineageReservation, error) {
	sources, err := canonicalDocumentLineageURLs(urls)
	if err != nil {
		return nil, err
	}
	release, err := d.outboundBoundaries.lockWrites(ctx, sources)
	if err != nil {
		return nil, fmt.Errorf("reserve document lineages: %w", err)
	}
	covered := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		covered[source] = struct{}{}
	}

	return &documentLineageLease{
		boundaries: d.outboundBoundaries,
		sources:    covered,
		release:    release,
	}, nil
}

func (d documentVault) ReleaseDocumentLineages(reservation DocumentLineageReservation) {
	lease, ok := reservation.(*documentLineageLease)
	if !ok || lease == nil || lease.boundaries != d.outboundBoundaries {
		return
	}
	lease.close()
}

func (l *documentLineageLease) close() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.released.Store(true)
		l.release()
	})
}

func (d documentVault) activeDocumentLineageLease(
	reservation DocumentLineageReservation,
	sources []string,
) error {
	lease, ok := reservation.(*documentLineageLease)
	if !ok || lease == nil || lease.boundaries != d.outboundBoundaries ||
		lease.released.Load() {
		return fmt.Errorf("document lineage reservation is not active")
	}
	canonical, err := canonicalDocumentLineageURLs(sources)
	if err != nil {
		return err
	}
	for _, source := range canonical {
		if _, covered := lease.sources[source]; !covered {
			return fmt.Errorf("document lineage reservation does not cover source")
		}
	}

	return nil
}

func (d documentVault) CanonicalReservedDocuments(
	ctx context.Context,
	reservation DocumentLineageReservation,
	docs []Document,
) ([]Document, error) {
	prepared := make([]Document, len(docs))
	urls := make([]string, 0, len(docs))
	for index, document := range docs {
		prepared[index] = retainSubmittedInlinks(normalizedDocument(document))
		urls = append(urls, prepared[index].NormalizedURL)
	}
	if err := d.activeDocumentLineageLease(reservation, urls); err != nil {
		return nil, err
	}
	canonical := make([]Document, 0, len(docs))
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		for _, document := range prepared {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context: %w", err)
			}
			stored, _, accepted, _, err := d.canonicalDocument(tx, document, nil)
			if err != nil {
				return err
			}
			if accepted {
				canonical = append(canonical, stored)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("canonical reserved documents: %w", err)
	}

	return canonical, nil
}

func canonicalDocumentLineageURLs(urls []string) ([]string, error) {
	seen := make(map[string]struct{}, len(urls))
	canonical := make([]string, 0, len(urls))
	for _, rawURL := range urls {
		normalizedURL := strings.TrimSpace(rawURL)
		if normalizedURL == "" {
			continue
		}
		if !validOutboundAnchorIdentity(normalizedURL) {
			return nil, fmt.Errorf("document lineage URL is invalid")
		}
		if _, duplicate := seen[normalizedURL]; duplicate {
			continue
		}
		seen[normalizedURL] = struct{}{}
		canonical = append(canonical, normalizedURL)
	}
	sort.Strings(canonical)

	return canonical, nil
}

var (
	_ DocumentLineageReserver            = documentVault{}
	_ ReservedOutboundAnchorReceiver     = documentVault{}
	_ ReservedCanonicalDocumentDirectory = documentVault{}
)
