package documentstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStoredDocumentWriteBoundaryCanceledScanReleasesLaterWrites(t *testing.T) {
	boundary := newStoredDocumentWriteBoundary()
	releaseActive, err := boundary.enterWrite(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	scanContext, cancelScan := context.WithCancel(t.Context())
	scanResult := make(chan error, 1)
	go func() {
		release, err := boundary.enterScan(scanContext)
		if err == nil {
			release()
		}
		scanResult <- err
	}()
	waitForStoredDocumentPendingScan(t, boundary)
	laterWrite := make(chan func(), 1)
	go func() {
		release, err := boundary.enterWrite(t.Context())
		if err != nil {
			laterWrite <- nil

			return
		}
		laterWrite <- release
	}()
	select {
	case <-laterWrite:
		t.Fatal("later write crossed pending scan")
	case <-time.After(25 * time.Millisecond):
	}
	cancelScan()
	if err := <-scanResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("scan cancellation = %v", err)
	}
	select {
	case release := <-laterWrite:
		if release == nil {
			t.Fatal("later write failed")
		}
		release()
	case <-time.After(time.Second):
		t.Fatal("later write remained blocked after scan cancellation")
	}
	releaseActive()
}

func TestStoredDocumentWriteBoundaryPrefersPendingScan(t *testing.T) {
	boundary := newStoredDocumentWriteBoundary()
	releaseActive, err := boundary.enterWrite(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	scanAcquired := make(chan func(), 1)
	go func() {
		release, err := boundary.enterScan(t.Context())
		if err != nil {
			scanAcquired <- nil

			return
		}
		scanAcquired <- release
	}()
	waitForStoredDocumentPendingScan(t, boundary)
	writeAcquired := make(chan func(), 1)
	go func() {
		release, err := boundary.enterWrite(t.Context())
		if err != nil {
			writeAcquired <- nil

			return
		}
		writeAcquired <- release
	}()
	releaseActive()
	var releaseScan func()
	select {
	case releaseScan = <-scanAcquired:
		if releaseScan == nil {
			t.Fatal("scan acquisition failed")
		}
	case <-time.After(time.Second):
		t.Fatal("pending scan did not acquire")
	}
	select {
	case <-writeAcquired:
		t.Fatal("later write crossed active scan")
	case <-time.After(25 * time.Millisecond):
	}
	releaseScan()
	select {
	case releaseWrite := <-writeAcquired:
		if releaseWrite == nil {
			t.Fatal("later write acquisition failed")
		}
		releaseWrite()
	case <-time.After(time.Second):
		t.Fatal("later write did not acquire after scan")
	}
}

func TestStoredDocumentWriteBoundaryHonorsCanceledEntry(t *testing.T) {
	boundary := newStoredDocumentWriteBoundary()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := boundary.enterWrite(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("write cancellation = %v", err)
	}
	if _, err := boundary.enterScan(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan cancellation = %v", err)
	}
	if _, err := (documentVault{}).enterStoredDocumentWrite(
		ctx,
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("nil boundary write cancellation = %v", err)
	}
	if _, err := (documentVault{}).enterStoredDocumentScanBoundary(
		ctx,
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("nil boundary scan cancellation = %v", err)
	}
}

func TestDisjointDocumentWritesReachStorageConcurrently(t *testing.T) {
	_, receiver, engine := openPagedDocuments(t)
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: "https://example.org/seed",
	}}); err != nil {
		t.Fatal(err)
	}
	documents := receiver.(documentVault)
	const writers = 24
	urls := distinctStoredDocumentBoundaryURLs(
		documents.urlBoundaries,
		writers,
		"https://disjoint.example/",
	)
	releaseUpdates := make(chan struct{})
	allEntered := make(chan struct{})
	var entered atomic.Int64
	engine.beforeUpdate = func() {
		if entered.Add(1) == writers {
			close(allEntered)
		}
		<-releaseUpdates
	}
	start := make(chan struct{})
	results := make(chan error, writers)
	var group sync.WaitGroup
	for _, normalizedURL := range urls {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := receiver.Receive(t.Context(), []Document{{
				NormalizedURL: normalizedURL,
			}})
			results <- err
		}()
	}
	close(start)
	select {
	case <-allEntered:
	case <-time.After(time.Second):
		close(releaseUpdates)
		t.Fatalf("concurrent storage entries = %d, want %d", entered.Load(), writers)
	}
	close(releaseUpdates)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func waitForStoredDocumentPendingScan(
	t *testing.T,
	boundary *storedDocumentWriteBoundary,
) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		boundary.mutex.Lock()
		waiting := boundary.waitingScans
		boundary.mutex.Unlock()
		if waiting > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("scan did not become pending")
		}
		time.Sleep(time.Millisecond)
	}
}

func distinctStoredDocumentBoundaryURLs(
	_ *storedDocumentURLBoundaries,
	total int,
	prefix string,
) []string {
	urls := make([]string, 0, total)
	for sequence := range total {
		urls = append(urls, fmt.Sprintf("%s%d", prefix, sequence))
	}

	return urls
}
