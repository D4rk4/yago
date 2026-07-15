package documentstore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

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
