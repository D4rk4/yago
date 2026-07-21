package documentstore

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

type storedDocumentURLBoundary struct {
	mutex          sync.Mutex
	readers        int
	writer         bool
	waiters        []*storedDocumentURLBoundaryWaiter
	waitingWriters int
	references     int
	url            string
	idlePrevious   *storedDocumentURLBoundary
	idleNext       *storedDocumentURLBoundary
	idle           bool
}

type storedDocumentURLBoundaryWaiter struct {
	write bool
	ready chan struct{}
}

func (b *storedDocumentURLBoundary) enterRead(
	ctx context.Context,
) (func(), error) {
	b.mutex.Lock()
	if err := ctx.Err(); err != nil {
		b.mutex.Unlock()

		return nil, fmt.Errorf("read document URL boundary: %w", err)
	}
	if !b.writer && len(b.waiters) == 0 {
		b.readers++
		b.mutex.Unlock()

		return b.readRelease(), nil
	}
	waiter := &storedDocumentURLBoundaryWaiter{ready: make(chan struct{})}
	b.waiters = append(b.waiters, waiter)
	b.grantWaiters()
	b.mutex.Unlock()
	if err := b.wait(ctx, waiter); err != nil {
		return nil, fmt.Errorf("read document URL boundary: %w", err)
	}

	return b.readRelease(), nil
}

func (b *storedDocumentURLBoundary) enterWrite(
	ctx context.Context,
) (func(), error) {
	b.mutex.Lock()
	if err := ctx.Err(); err != nil {
		b.mutex.Unlock()

		return nil, fmt.Errorf("write document URL boundary: %w", err)
	}
	if !b.writer && b.readers == 0 && len(b.waiters) == 0 {
		b.writer = true
		b.mutex.Unlock()

		return b.writeRelease(), nil
	}
	waiter := &storedDocumentURLBoundaryWaiter{
		write: true,
		ready: make(chan struct{}),
	}
	b.waiters = append(b.waiters, waiter)
	b.waitingWriters++
	b.grantWaiters()
	b.mutex.Unlock()
	if err := b.wait(ctx, waiter); err != nil {
		return nil, fmt.Errorf("write document URL boundary: %w", err)
	}

	return b.writeRelease(), nil
}

func (b *storedDocumentURLBoundary) wait(
	ctx context.Context,
	waiter *storedDocumentURLBoundaryWaiter,
) error {
	select {
	case <-waiter.ready:
		if err := ctx.Err(); err != nil {
			b.releaseGranted(waiter.write)

			return fmt.Errorf("document URL boundary wait: %w", err)
		}

		return nil
	case <-ctx.Done():
	}
	b.cancelWaiter(waiter)

	return fmt.Errorf("document URL boundary wait: %w", ctx.Err())
}

func (b *storedDocumentURLBoundary) cancelWaiter(
	waiter *storedDocumentURLBoundaryWaiter,
) {
	b.mutex.Lock()
	removed := b.removeWaiter(waiter)
	if removed {
		b.grantWaiters()
	}
	b.mutex.Unlock()
	if !removed {
		b.releaseGranted(waiter.write)
	}
}

func (b *storedDocumentURLBoundary) removeWaiter(
	waiter *storedDocumentURLBoundaryWaiter,
) bool {
	for index, candidate := range b.waiters {
		if candidate != waiter {
			continue
		}
		b.waiters = slices.Delete(b.waiters, index, index+1)
		if waiter.write {
			b.waitingWriters--
		}

		return true
	}

	return false
}

func (b *storedDocumentURLBoundary) grantWaiters() {
	if b.writer || b.readers > 0 || len(b.waiters) == 0 {
		return
	}
	if b.waiters[0].write {
		waiter := b.waiters[0]
		b.waiters = b.waiters[1:]
		b.waitingWriters--
		b.writer = true
		close(waiter.ready)

		return
	}
	for len(b.waiters) > 0 && !b.waiters[0].write {
		waiter := b.waiters[0]
		b.waiters = b.waiters[1:]
		b.readers++
		close(waiter.ready)
	}
}

func (b *storedDocumentURLBoundary) readRelease() func() {
	var released sync.Once

	return func() {
		released.Do(func() { b.releaseGranted(false) })
	}
}

func (b *storedDocumentURLBoundary) writeRelease() func() {
	var released sync.Once

	return func() {
		released.Do(func() { b.releaseGranted(true) })
	}
}

func (b *storedDocumentURLBoundary) releaseGranted(write bool) {
	b.mutex.Lock()
	if write {
		b.writer = false
	} else {
		b.readers--
	}
	b.grantWaiters()
	b.mutex.Unlock()
}
