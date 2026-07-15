package documentstore

import (
	"context"
	"fmt"
	"sync"
)

type storedDocumentWriteBoundary struct {
	mutex          sync.Mutex
	changed        chan struct{}
	activeWrites   int
	waitingScans   int
	scanInProgress bool
}

func newStoredDocumentWriteBoundary() *storedDocumentWriteBoundary {
	return &storedDocumentWriteBoundary{changed: make(chan struct{})}
}

func (b *storedDocumentWriteBoundary) enterWrite(
	ctx context.Context,
) (func(), error) {
	b.mutex.Lock()
	b.ensureChanged()
	for {
		if err := ctx.Err(); err != nil {
			b.mutex.Unlock()

			return nil, fmt.Errorf("context: %w", err)
		}
		if !b.scanInProgress && b.waitingScans == 0 {
			b.activeWrites++
			b.mutex.Unlock()
			var released sync.Once

			return func() {
				released.Do(func() {
					b.mutex.Lock()
					b.activeWrites--
					b.broadcast()
					b.mutex.Unlock()
				})
			}, nil
		}
		changed := b.changed
		b.mutex.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, fmt.Errorf("context: %w", ctx.Err())
		}
		b.mutex.Lock()
	}
}

func (b *storedDocumentWriteBoundary) enterScan(
	ctx context.Context,
) (func(), error) {
	b.mutex.Lock()
	b.ensureChanged()
	if err := ctx.Err(); err != nil {
		b.mutex.Unlock()

		return nil, fmt.Errorf("context: %w", err)
	}
	b.waitingScans++
	for b.activeWrites > 0 || b.scanInProgress {
		if err := ctx.Err(); err != nil {
			b.waitingScans--
			b.broadcast()
			b.mutex.Unlock()

			return nil, fmt.Errorf("context: %w", err)
		}
		changed := b.changed
		b.mutex.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		b.mutex.Lock()
	}
	if err := ctx.Err(); err != nil {
		b.waitingScans--
		b.broadcast()
		b.mutex.Unlock()

		return nil, fmt.Errorf("context: %w", err)
	}
	b.waitingScans--
	b.scanInProgress = true
	b.mutex.Unlock()
	var released sync.Once

	return func() {
		released.Do(func() {
			b.mutex.Lock()
			b.scanInProgress = false
			b.broadcast()
			b.mutex.Unlock()
		})
	}, nil
}

func (b *storedDocumentWriteBoundary) ensureChanged() {
	if b.changed == nil {
		b.changed = make(chan struct{})
	}
}

func (b *storedDocumentWriteBoundary) broadcast() {
	b.ensureChanged()
	close(b.changed)
	b.changed = make(chan struct{})
}

func (d documentVault) enterStoredDocumentWrite(
	ctx context.Context,
) (func(), error) {
	if d.writeBoundary == nil {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wait for stored document write boundary: %w", err)
		}

		return func() {}, nil
	}
	release, err := d.writeBoundary.enterWrite(ctx)
	if err != nil {
		return nil, fmt.Errorf("wait for stored document write boundary: %w", err)
	}

	return release, nil
}

func (d documentVault) enterStoredDocumentScanBoundary(
	ctx context.Context,
) (func(), error) {
	if d.writeBoundary == nil {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wait for stored document scan boundary: %w", err)
		}

		return func() {}, nil
	}
	release, err := d.writeBoundary.enterScan(ctx)
	if err != nil {
		return nil, fmt.Errorf("wait for stored document scan boundary: %w", err)
	}

	return release, nil
}
