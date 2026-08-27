package documentstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestDocumentSetDirectoryUsesOneSnapshotAndPreservesOrder(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	first := Document{NormalizedURL: "https://example.org/first", Title: "first"}
	second := Document{NormalizedURL: "https://example.org/second", Title: "second"}
	if _, err := receiver.Receive(t.Context(), []Document{first, second}); err != nil {
		t.Fatal(err)
	}
	legacy := Document{NormalizedURL: "https://example.org/legacy", Title: "legacy"}
	encodedLegacy, err := (documentCodec{}).Encode(legacy)
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[bucketName][legacy.NormalizedURL] = encodedLegacy
	requested := []string{
		second.NormalizedURL,
		"https://example.org/missing",
		legacy.NormalizedURL,
		first.NormalizedURL,
		second.NormalizedURL,
	}
	before := engine.views
	documents, found, err := directory.(DocumentSetDirectory).Documents(
		t.Context(),
		requested,
	)
	if err != nil {
		t.Fatal(err)
	}
	if engine.views-before != 1 {
		t.Fatalf("document snapshots=%d, want=1", engine.views-before)
	}
	wantFound := []bool{true, false, true, true, true}
	wantTitles := []string{"second", "", "legacy", "first", "second"}
	if len(documents) != len(requested) || len(found) != len(requested) {
		t.Fatalf("document results=%d/%d, want=%d", len(documents), len(found), len(requested))
	}
	for index := range requested {
		if found[index] != wantFound[index] || documents[index].Title != wantTitles[index] {
			t.Fatalf(
				"document[%d]=%#v/%t, want title=%q/found=%t",
				index,
				documents[index],
				found[index],
				wantTitles[index],
				wantFound[index],
			)
		}
	}
}

func TestDocumentSetDirectoryEmptyInputDoesNotRead(t *testing.T) {
	directory, _, engine := openScriptedDocuments(t)
	before := engine.views
	documents, found, err := directory.(DocumentSetDirectory).Documents(t.Context(), nil)
	if err != nil || len(documents) != 0 || len(found) != 0 {
		t.Fatalf("empty documents=%v found=%v error=%v", documents, found, err)
	}
	if engine.views != before {
		t.Fatalf("empty document snapshots=%d, want=%d", engine.views, before)
	}
}

func TestDocumentSetDirectoryTreatsMismatchedRowsAsAbsent(t *testing.T) {
	tests := []struct {
		name    string
		ordered bool
	}{
		{name: "ordered", ordered: true},
		{name: "legacy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory, _, engine := openScriptedDocuments(t)
			requested := "https://example.org/requested"
			wrong := Document{NormalizedURL: "https://example.org/other"}
			encoded, err := (documentCodec{}).Encode(wrong)
			if err != nil {
				t.Fatal(err)
			}
			if test.ordered {
				seedScriptedDocumentLocation(t, engine, requested, 1)
				key, keyErr := orderedDocumentKey(1, requested)
				if keyErr != nil {
					t.Fatal(keyErr)
				}
				engine.buckets[orderedDocumentBucketName][string(key)] = encoded
			} else {
				engine.buckets[bucketName][requested] = encoded
			}
			documents, found, err := directory.(DocumentSetDirectory).Documents(
				t.Context(),
				[]string{requested},
			)
			if err != nil || len(documents) != 1 || len(found) != 1 || found[0] {
				t.Fatalf("mismatched documents=%v found=%v error=%v", documents, found, err)
			}
		})
	}
}

func TestDocumentSetDirectoryRetainsCorruptRowCompatibility(t *testing.T) {
	tests := []struct {
		name    string
		ordered bool
	}{
		{name: "ordered", ordered: true},
		{name: "legacy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory, receiver, engine := openScriptedDocuments(t)
			valid := Document{NormalizedURL: "https://example.org/valid", Title: "unchanged"}
			if _, err := receiver.Receive(t.Context(), []Document{valid}); err != nil {
				t.Fatal(err)
			}
			corrupt := "https://example.org/corrupt"
			if test.ordered {
				seedScriptedDocumentLocation(t, engine, corrupt, 2)
				key, err := orderedDocumentKey(2, corrupt)
				if err != nil {
					t.Fatal(err)
				}
				engine.buckets[orderedDocumentBucketName][string(key)] = []byte("{")
			} else {
				engine.buckets[bucketName][corrupt] = []byte("{")
			}
			documents, found, err := directory.(DocumentSetDirectory).Documents(
				t.Context(),
				[]string{corrupt, valid.NormalizedURL},
			)
			if err != nil || found[0] || !found[1] || documents[1].Title != valid.Title {
				t.Fatalf("compatible documents=%v found=%v error=%v", documents, found, err)
			}
		})
	}
}

func TestDocumentSetDirectoryRejectsMalformedLocation(t *testing.T) {
	directory, _, engine := openScriptedDocuments(t)
	url := "https://example.org/malformed-location"
	engine.buckets[documentLocationBucketName][url] = []byte{1}
	if _, _, err := directory.(DocumentSetDirectory).Documents(
		t.Context(),
		[]string{url},
	); err == nil {
		t.Fatal("malformed document location accepted")
	}
}

func TestDocumentSetDirectoryRejectsOversizedOrderedIdentity(t *testing.T) {
	directory, _, engine := openScriptedDocuments(t)
	url := strings.Repeat("x", yagomodel.MaximumURLIdentityBytes+1)
	seedScriptedDocumentLocation(t, engine, url, 1)
	if _, _, err := directory.(DocumentSetDirectory).Documents(
		t.Context(),
		[]string{url},
	); err == nil {
		t.Fatal("oversized ordered document identity accepted")
	}
}

func TestDocumentSetDirectoryRejectsOperationalBatchFailure(t *testing.T) {
	storage, documents, _ := openDocumentStorageFaultVault(t)
	failure := errors.New("batch read failed")
	err := storage.View(t.Context(), func(tx *vault.Txn) error {
		_, _, readErr := documents.readStoredDocumentSetAfterBatchFailure(
			t.Context(),
			tx,
			[]string{"https://example.org/failure"},
			"read ordered documents",
			failure,
		)

		return readErr
	})
	if !errors.Is(err, failure) {
		t.Fatalf("batch failure=%v, want=%v", err, failure)
	}
}

func TestDocumentSetDirectoryRejectsCancellationInsideCorruptFallback(t *testing.T) {
	storage, documents, _ := openDocumentStorageFaultVault(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := storage.View(t.Context(), func(tx *vault.Txn) error {
		_, _, readErr := documents.readStoredDocumentSetAfterBatchFailure(
			ctx,
			tx,
			[]string{"https://example.org/cancelled"},
			"read ordered documents",
			vault.ErrCorruptValue,
		)

		return readErr
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("corrupt fallback cancellation=%v", err)
	}
}

func TestDocumentSetDirectoryReportsCorruptFallbackReadFailure(t *testing.T) {
	storage, documents, engine := openDocumentStorageFaultVault(t)
	url := "https://example.org/read-failure"
	raw, err := (documentCodec{}).Encode(Document{NormalizedURL: url})
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(bucketName, vault.Key(url), raw)
	failure := errors.New("individual read failed")
	engine.readErrors[bucketName] = failure
	err = storage.View(t.Context(), func(tx *vault.Txn) error {
		_, _, readErr := documents.readStoredDocumentSetAfterBatchFailure(
			t.Context(),
			tx,
			[]string{url},
			"read legacy documents",
			vault.ErrCorruptValue,
		)

		return readErr
	})
	if !errors.Is(err, failure) {
		t.Fatalf("fallback read failure=%v, want=%v", err, failure)
	}
}

func TestDocumentSetDirectoryCancelsAtURLBoundary(t *testing.T) {
	directory, _, _ := openPagedDocuments(t)
	documents := directory.(documentVault)
	url := "https://locks.example/document-set"
	releaseWrite, err := documents.urlBoundaries.lockWrites(t.Context(), []string{url})
	if err != nil {
		t.Fatal(err)
	}
	defer releaseWrite()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, _, err := directory.(DocumentSetDirectory).Documents(ctx, []string{url})
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("document set returned before cancellation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("document set cancellation=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("document set ignored cancellation")
	}
}

func TestDocumentSetDirectoryRejectsCancellationDuringCorruptFallback(t *testing.T) {
	directory, _, engine := openScriptedDocuments(t)
	url := "https://example.org/corrupt"
	engine.buckets[bucketName][url] = []byte("{")
	ctx := &errAfterContext{
		Context:   context.Background(),
		remaining: 4,
		err:       context.Canceled,
	}
	_, _, err := directory.(DocumentSetDirectory).Documents(ctx, []string{url, url})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fallback cancellation=%v", err)
	}
}

func TestDocumentSetDirectoryRejectsClosedVault(t *testing.T) {
	engine := newScriptedDocumentEngine()
	vaultStore, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	directory, _, err := Open(vaultStore)
	if err != nil {
		t.Fatal(err)
	}
	if err := vaultStore.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := directory.(DocumentSetDirectory).Documents(
		t.Context(),
		[]string{"https://example.org/closed"},
	); err == nil {
		t.Fatal("closed vault accepted document set read")
	}
}
