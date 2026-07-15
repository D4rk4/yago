package documentstore

import (
	"context"
	"fmt"
	"hash/fnv"
	"slices"
	"sync"
)

const storedDocumentURLBoundaryTotal = 4096

type storedDocumentURLBoundaries struct {
	entries [storedDocumentURLBoundaryTotal]storedDocumentURLBoundary
}

type storedDocumentURLBoundary struct {
	mutex          sync.Mutex
	changed        chan struct{}
	readers        int
	writer         bool
	waitingWriters int
}

func (b *storedDocumentURLBoundaries) lockReads(
	ctx context.Context,
	urls []string,
) (func(), error) {
	indices := b.indices(urls)
	releases := make([]func(), 0, len(indices))
	for _, index := range indices {
		release, err := b.entries[index].enterRead(ctx)
		if err != nil {
			releaseStoredDocumentURLBoundaries(releases)

			return nil, fmt.Errorf("wait for document URL read boundary: %w", err)
		}
		releases = append(releases, release)
	}
	var released sync.Once

	return func() {
		released.Do(func() { releaseStoredDocumentURLBoundaries(releases) })
	}, nil
}

func (b *storedDocumentURLBoundaries) lockWrites(
	ctx context.Context,
	urls []string,
) (func(), error) {
	indices := b.indices(urls)
	releases := make([]func(), 0, len(indices))
	for _, index := range indices {
		release, err := b.entries[index].enterWrite(ctx)
		if err != nil {
			releaseStoredDocumentURLBoundaries(releases)

			return nil, fmt.Errorf("wait for document URL write boundary: %w", err)
		}
		releases = append(releases, release)
	}
	var released sync.Once

	return func() {
		released.Do(func() { releaseStoredDocumentURLBoundaries(releases) })
	}, nil
}

func (b *storedDocumentURLBoundaries) indices(urls []string) []int {
	unique := make(map[int]struct{}, len(urls))
	for _, url := range urls {
		if url == "" {
			continue
		}
		key := fnv.New64a()
		_, _ = key.Write([]byte(url))
		unique[int(key.Sum64()%storedDocumentURLBoundaryTotal)] = struct{}{}
	}
	indices := make([]int, 0, len(unique))
	for index := range unique {
		indices = append(indices, index)
	}
	slices.Sort(indices)

	return indices
}

func (b *storedDocumentURLBoundary) enterRead(
	ctx context.Context,
) (func(), error) {
	b.mutex.Lock()
	b.ensureChanged()
	for b.writer || b.waitingWriters > 0 {
		changed := b.changed
		b.mutex.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, fmt.Errorf("read document URL boundary: %w", ctx.Err())
		}
		b.mutex.Lock()
	}
	if err := ctx.Err(); err != nil {
		b.mutex.Unlock()

		return nil, fmt.Errorf("read document URL boundary: %w", err)
	}
	b.readers++
	b.mutex.Unlock()

	return func() {
		b.mutex.Lock()
		b.readers--
		b.broadcast()
		b.mutex.Unlock()
	}, nil
}

func (b *storedDocumentURLBoundary) enterWrite(
	ctx context.Context,
) (func(), error) {
	b.mutex.Lock()
	b.ensureChanged()
	if err := ctx.Err(); err != nil {
		b.mutex.Unlock()

		return nil, fmt.Errorf("write document URL boundary: %w", err)
	}
	b.waitingWriters++
	for b.writer || b.readers > 0 {
		changed := b.changed
		b.mutex.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		b.mutex.Lock()
		if err := ctx.Err(); err != nil {
			b.waitingWriters--
			b.broadcast()
			b.mutex.Unlock()

			return nil, fmt.Errorf("write document URL boundary: %w", err)
		}
	}
	b.waitingWriters--
	b.writer = true
	b.mutex.Unlock()

	return func() {
		b.mutex.Lock()
		b.writer = false
		b.broadcast()
		b.mutex.Unlock()
	}, nil
}

func (b *storedDocumentURLBoundary) ensureChanged() {
	if b.changed == nil {
		b.changed = make(chan struct{})
	}
}

func (b *storedDocumentURLBoundary) broadcast() {
	b.ensureChanged()
	close(b.changed)
	b.changed = make(chan struct{})
}

func releaseStoredDocumentURLBoundaries(releases []func()) {
	for _, release := range slices.Backward(releases) {
		release()
	}
}
