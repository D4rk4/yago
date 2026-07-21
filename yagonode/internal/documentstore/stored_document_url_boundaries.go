package documentstore

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

func (b *storedDocumentURLBoundaries) lockReads(
	ctx context.Context,
	urls []string,
) (func(), error) {
	return b.lock(ctx, urls, false)
}

func (b *storedDocumentURLBoundaries) lockWrites(
	ctx context.Context,
	urls []string,
) (func(), error) {
	return b.lock(ctx, urls, true)
}

func (b *storedDocumentURLBoundaries) lock(
	ctx context.Context,
	urls []string,
	write bool,
) (func(), error) {
	leases := b.retain(urls)
	releases := make([]func(), 0, len(leases))
	for _, lease := range leases {
		var release func()
		var err error
		if write {
			release, err = lease.boundary.enterWrite(ctx)
		} else {
			release, err = lease.boundary.enterRead(ctx)
		}
		if err != nil {
			releaseStoredDocumentURLBoundaries(releases)
			b.release(leases)
			mode := "read"
			if write {
				mode = "write"
			}

			return nil, fmt.Errorf("wait for document URL %s boundary: %w", mode, err)
		}
		releases = append(releases, release)
	}
	var released sync.Once

	return func() {
		released.Do(func() {
			releaseStoredDocumentURLBoundaries(releases)
			b.release(leases)
		})
	}, nil
}

func releaseStoredDocumentURLBoundaries(releases []func()) {
	for _, release := range slices.Backward(releases) {
		release()
	}
}
