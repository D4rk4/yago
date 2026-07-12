package documentstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStoredDocumentScansAreSerialized(t *testing.T) {
	directory, receiver := openDocuments(t)
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: "https://example.org/",
	}}); err != nil {
		t.Fatal(err)
	}
	source := directory.(StoredDocuments)
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- source.StoredDocuments(t.Context(), func(Document) (bool, error) {
			close(firstEntered)
			<-releaseFirst

			return false, nil
		})
	}()
	<-firstEntered

	waitContext, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	secondVisited := false
	err := source.StoredDocuments(waitContext, func(Document) (bool, error) {
		secondVisited = true

		return false, nil
	})
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(err, context.DeadlineExceeded) || secondVisited {
		t.Fatalf("concurrent scan visited/error = %t/%v", secondVisited, err)
	}
	if err := source.StoredDocuments(t.Context(), func(Document) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStoredDocumentScanWaitHonorsCancellation(t *testing.T) {
	directory := openStoredDocumentScanDirectory(t)
	vaultDirectory := directory.(documentVault)
	release, err := vaultDirectory.enterStoredDocumentScan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = directory.(StoredDocuments).StoredDocuments(
		ctx,
		func(Document) (bool, error) {
			t.Fatal("canceled scan entered visitor")

			return false, nil
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scan error = %v", err)
	}
}

func TestNilStoredDocumentScanAdmissionAllowsScan(t *testing.T) {
	release, err := (documentVault{}).enterStoredDocumentScan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func openStoredDocumentScanDirectory(t *testing.T) DocumentDirectory {
	t.Helper()
	directory, _ := openDocuments(t)

	return directory
}
