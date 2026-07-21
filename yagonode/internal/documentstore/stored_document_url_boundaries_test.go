package documentstore

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"testing"
	"time"
)

func TestStoredDocumentURLBoundariesDoNotSerializeHashCollisions(t *testing.T) {
	boundaries := &storedDocumentURLBoundaries{}
	first, second := legacyStoredDocumentBoundaryCollision()
	releaseWrite, err := boundaries.lockWrites(t.Context(), []string{first})
	if err != nil {
		t.Fatal(err)
	}
	defer releaseWrite()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	releaseRead, err := boundaries.lockReads(ctx, []string{second})
	if err != nil {
		t.Fatalf("unrelated URL waited behind hash collision: %v", err)
	}
	releaseRead()
}

func TestStoredDocumentURLBoundariesPreserveArrivalOrder(t *testing.T) {
	boundaries := &storedDocumentURLBoundaries{}
	url := "https://locks.example/fair"
	releaseFirst, err := boundaries.lockWrites(t.Context(), []string{url})
	if err != nil {
		t.Fatal(err)
	}
	boundary := retainedStoredDocumentBoundary(t, boundaries, url)
	second := make(chan func(), 1)
	go func() {
		release, _ := boundaries.lockWrites(t.Context(), []string{url})
		second <- release
	}()
	waitForStoredDocumentBoundaryQueue(t, boundary, 1, 1)
	read := make(chan func(), 1)
	go func() {
		release, _ := boundaries.lockReads(t.Context(), []string{url})
		read <- release
	}()
	waitForStoredDocumentBoundaryQueue(t, boundary, 1, 2)
	third := make(chan func(), 1)
	go func() {
		release, _ := boundaries.lockWrites(t.Context(), []string{url})
		third <- release
	}()
	waitForStoredDocumentBoundaryQueue(t, boundary, 2, 3)
	releaseFirst()
	var releaseSecond func()
	select {
	case releaseSecond = <-second:
	case <-time.After(time.Second):
		t.Fatal("second writer did not acquire")
	}
	releaseSecond()
	var releaseRead func()
	select {
	case releaseRead = <-read:
	case <-third:
		t.Fatal("later writer crossed queued read")
	case <-time.After(time.Second):
		t.Fatal("queued read did not acquire")
	}
	select {
	case <-third:
		t.Fatal("writer crossed active read")
	case <-time.After(25 * time.Millisecond):
	}
	releaseRead()
	select {
	case releaseThird := <-third:
		releaseThird()
	case <-time.After(time.Second):
		t.Fatal("third writer did not acquire")
	}
	boundaries.mutex.Lock()
	active := 0
	for _, boundary := range boundaries.entries {
		active += boundary.references
	}
	cached := len(boundaries.entries)
	boundaries.mutex.Unlock()
	if active != 0 || cached > storedDocumentURLBoundaryCacheSize {
		t.Fatalf("released URL boundaries active/cached = %d/%d", active, cached)
	}
}

func TestStoredDocumentURLBoundaryCacheIsBounded(t *testing.T) {
	boundaries := &storedDocumentURLBoundaries{}
	for sequence := 0; sequence <= storedDocumentURLBoundaryCacheSize; sequence++ {
		url := fmt.Sprintf("https://cache.example/%d", sequence)
		release, err := boundaries.lockReads(t.Context(), []string{url})
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	boundaries.mutex.Lock()
	defer boundaries.mutex.Unlock()
	if len(boundaries.entries) != storedDocumentURLBoundaryCacheSize ||
		boundaries.idleTotal != storedDocumentURLBoundaryCacheSize {
		t.Fatalf(
			"cached URL boundaries = %d/%d",
			len(boundaries.entries),
			boundaries.idleTotal,
		)
	}
	if boundaries.entries["https://cache.example/0"] != nil {
		t.Fatal("oldest idle URL boundary was not evicted")
	}
	if boundaries.entries[fmt.Sprintf(
		"https://cache.example/%d",
		storedDocumentURLBoundaryCacheSize,
	)] == nil {
		t.Fatal("newest idle URL boundary was evicted")
	}
}

func TestStoredDocumentURLBoundaryCancellationRacesReleaseGrantedLocks(t *testing.T) {
	boundary := &storedDocumentURLBoundary{readers: 1}
	ready := make(chan struct{})
	close(ready)
	waiter := &storedDocumentURLBoundaryWaiter{ready: ready}
	ctx := storedDocumentReadyErrorContext{Context: context.Background()}
	if err := boundary.wait(ctx, waiter); !errors.Is(err, context.Canceled) {
		t.Fatalf("granted read cancellation = %v", err)
	}
	if boundary.readers != 0 {
		t.Fatalf("canceled granted readers = %d", boundary.readers)
	}
	queued := &storedDocumentURLBoundaryWaiter{ready: make(chan struct{})}
	boundary.waiters = []*storedDocumentURLBoundaryWaiter{queued}
	boundary.writer = true
	boundary.cancelWaiter(&storedDocumentURLBoundaryWaiter{
		write: true,
		ready: make(chan struct{}),
	})
	if boundary.writer {
		t.Fatal("canceled granted writer remained active")
	}
}

func TestStoredDocumentURLBoundariesReverseOrderDoesNotDeadlock(t *testing.T) {
	boundaries := &storedDocumentURLBoundaries{}
	urls := distinctStoredDocumentBoundaryURLs(boundaries, 2, "https://locks.example/")
	firstAcquired := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	go func() {
		release, err := boundaries.lockWrites(t.Context(), []string{urls[0], urls[1]})
		if err != nil {
			t.Error(err)

			return
		}
		close(firstAcquired)
		<-releaseFirst
		release()
		close(firstDone)
	}()
	<-firstAcquired
	secondAcquired := make(chan struct{})
	go func() {
		release, err := boundaries.lockWrites(t.Context(), []string{urls[1], urls[0]})
		if err != nil {
			t.Error(err)

			return
		}
		close(secondAcquired)
		release()
	}()
	select {
	case <-secondAcquired:
		t.Fatal("reverse lock crossed held boundary")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseFirst)
	select {
	case <-secondAcquired:
	case <-time.After(time.Second):
		t.Fatal("reverse lock order deadlocked")
	}
	<-firstDone
}

func TestPointReadsCancelWhileURLWriteBoundaryIsHeld(t *testing.T) {
	directory, _, _ := openPagedDocuments(t)
	documents := directory.(documentVault)
	url := "https://locks.example/canceled-read"
	releaseWrite, err := documents.urlBoundaries.lockWrites(t.Context(), []string{url})
	if err != nil {
		t.Fatal(err)
	}
	defer releaseWrite()
	tests := []struct {
		name string
		read func(context.Context) error
	}{
		{
			name: "document",
			read: func(ctx context.Context) error {
				_, _, err := directory.Document(ctx, url)
				if err != nil {
					return fmt.Errorf("read document: %w", err)
				}

				return nil
			},
		},
		{
			name: "presence",
			read: func(ctx context.Context) error {
				_, err := directory.(DocumentPresence).DocumentExists(ctx, url)
				if err != nil {
					return fmt.Errorf("read document presence: %w", err)
				}

				return nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			result := make(chan error, 1)
			go func() { result <- test.read(ctx) }()
			select {
			case err := <-result:
				t.Fatalf("point read returned before cancellation: %v", err)
			case <-time.After(25 * time.Millisecond):
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("point read cancellation = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("point read ignored cancellation")
			}
		})
	}
}

func TestOutboundSourceLockHonorsCancellation(t *testing.T) {
	_, receiver, _ := openPagedDocuments(t)
	documents := receiver.(documentVault)
	source := "https://locks.example/canceled-source"
	releaseSource, err := documents.outboundBoundaries.lockWrites(
		t.Context(),
		[]string{source},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseSource()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := receiver.(InboundAnchorReceiver).ReplaceOutboundAnchors(
			ctx,
			[]OutboundAnchorSet{{SourceURL: source}},
		)
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("outbound update returned before cancellation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("outbound cancellation = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("outbound source lock ignored cancellation")
	}
}

func TestStoredDocumentURLBoundariesReadAndWriteSerialization(t *testing.T) {
	boundaries := &storedDocumentURLBoundaries{}
	url := "https://locks.example/shared"
	releaseRead, err := boundaries.lockReads(t.Context(), []string{url, "", url})
	if err != nil {
		t.Fatal(err)
	}
	writeAcquired := make(chan func(), 1)
	go func() {
		release, err := boundaries.lockWrites(t.Context(), []string{url})
		if err != nil {
			writeAcquired <- nil

			return
		}
		writeAcquired <- release
	}()
	select {
	case <-writeAcquired:
		t.Fatal("write crossed read lock")
	case <-time.After(25 * time.Millisecond):
	}
	releaseRead()
	select {
	case releaseWrite := <-writeAcquired:
		if releaseWrite == nil {
			t.Fatal("write lock failed")
		}
		releaseWrite()
	case <-time.After(time.Second):
		t.Fatal("write remained blocked after read release")
	}
}

func BenchmarkStoredDocumentURLBoundariesDisjointHashCollision(b *testing.B) {
	boundaries := &storedDocumentURLBoundaries{}
	first, second := legacyStoredDocumentBoundaryCollision()
	releaseWrite, err := boundaries.lockWrites(context.Background(), []string{first})
	if err != nil {
		b.Fatal(err)
	}
	defer releaseWrite()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		releaseRead, err := boundaries.lockReads(context.Background(), []string{second})
		if err != nil {
			b.Fatal(err)
		}
		releaseRead()
	}
}

func legacyStoredDocumentBoundaryCollision() (string, string) {
	const boundaryTotal = 4096
	seen := make(map[uint64]string, boundaryTotal)
	for sequence := 0; ; sequence++ {
		url := fmt.Sprintf("https://collision.example/%d", sequence)
		key := fnv.New64a()
		_, _ = key.Write([]byte(url))
		boundary := key.Sum64() % boundaryTotal
		if previous := seen[boundary]; previous != "" {
			return previous, url
		}
		seen[boundary] = url
	}
}

func retainedStoredDocumentBoundary(
	t *testing.T,
	boundaries *storedDocumentURLBoundaries,
	url string,
) *storedDocumentURLBoundary {
	t.Helper()
	boundaries.mutex.Lock()
	defer boundaries.mutex.Unlock()
	boundary := boundaries.entries[url]
	if boundary == nil {
		t.Fatal("active URL boundary is absent")
	}

	return boundary
}

func waitForStoredDocumentBoundaryQueue(
	t *testing.T,
	boundary *storedDocumentURLBoundary,
	writers int,
	total int,
) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		boundary.mutex.Lock()
		waitingWriters := boundary.waitingWriters
		waitingTotal := len(boundary.waiters)
		boundary.mutex.Unlock()
		if waitingWriters == writers && waitingTotal == total {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"URL boundary queue = %d writers, %d total; want %d, %d",
				waitingWriters,
				waitingTotal,
				writers,
				total,
			)
		}
		time.Sleep(time.Millisecond)
	}
}

type storedDocumentReadyErrorContext struct {
	context.Context
}

func (storedDocumentReadyErrorContext) Err() error {
	return context.Canceled
}
