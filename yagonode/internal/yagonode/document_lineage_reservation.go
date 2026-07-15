package yagonode

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
)

type reservedDocumentLineageEviction struct {
	once           sync.Once
	mutex          sync.Mutex
	released       bool
	evictor        documentLineageEvictor
	lineage        documentstore.DocumentLineageReservation
	normalizedURLs map[string]struct{}
}

func (d documentLineageEvictor) ReserveDocumentEvictions(
	ctx context.Context,
	normalizedURLs []string,
) (eviction.ReservedDocumentEviction, error) {
	covered := make(map[string]struct{}, len(normalizedURLs))
	canonical := make([]string, 0, len(normalizedURLs))
	for _, rawURL := range normalizedURLs {
		normalizedURL := strings.TrimSpace(rawURL)
		if normalizedURL == "" {
			continue
		}
		if _, duplicate := covered[normalizedURL]; duplicate {
			continue
		}
		covered[normalizedURL] = struct{}{}
		canonical = append(canonical, normalizedURL)
	}
	var lineage documentstore.DocumentLineageReservation
	if d.lineages != nil {
		var err error
		lineage, err = d.lineages.ReserveDocumentLineages(ctx, canonical)
		if err != nil {
			return nil, fmt.Errorf("reserve document lineages: %w", err)
		}
	}

	return &reservedDocumentLineageEviction{
		evictor:        d,
		lineage:        lineage,
		normalizedURLs: covered,
	}, nil
}

func (r *reservedDocumentLineageEviction) Delete(
	ctx context.Context,
	normalizedURL string,
) (bool, error) {
	if r == nil {
		return false, fmt.Errorf("document eviction reservation is not active")
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.released {
		return false, fmt.Errorf("document eviction reservation is not active")
	}
	normalizedURL = strings.TrimSpace(normalizedURL)
	if _, covered := r.normalizedURLs[normalizedURL]; !covered {
		return false, fmt.Errorf("document eviction reservation does not cover URL")
	}

	return r.evictor.deleteReservedDocumentLineage(ctx, r.lineage, normalizedURL)
}

func (r *reservedDocumentLineageEviction) Release() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		r.mutex.Lock()
		defer r.mutex.Unlock()
		r.released = true
		if r.evictor.lineages != nil && r.lineage != nil {
			r.evictor.lineages.ReleaseDocumentLineages(r.lineage)
		}
	})
}

var _ eviction.ReservedDocumentEviction = (*reservedDocumentLineageEviction)(nil)
