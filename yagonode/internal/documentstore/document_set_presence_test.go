package documentstore

import (
	"context"
	"errors"
	"testing"
)

func TestDocumentSetPresenceUsesOneSnapshotAndPreservesOrder(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	first := "https://example.org/first"
	second := "https://example.org/second"
	if _, err := receiver.Receive(t.Context(), []Document{
		{NormalizedURL: first},
		{NormalizedURL: second},
	}); err != nil {
		t.Fatal(err)
	}
	before := engine.views
	found, err := directory.(DocumentSetPresence).DocumentsExist(
		t.Context(),
		[]string{first, "https://example.org/missing", second, first},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []bool{true, false, true, true}
	if len(found) != len(want) {
		t.Fatalf("presence length=%d, want=%d", len(found), len(want))
	}
	for index := range want {
		if found[index] != want[index] {
			t.Fatalf("presence[%d]=%t, want=%t", index, found[index], want[index])
		}
	}
	if engine.views-before != 1 {
		t.Fatalf("presence snapshots=%d, want=1", engine.views-before)
	}
}

func TestDocumentSetPresenceEmptyInputDoesNotRead(t *testing.T) {
	directory, _, engine := openScriptedDocuments(t)
	before := engine.views
	found, err := directory.(DocumentSetPresence).DocumentsExist(t.Context(), nil)
	if err != nil || len(found) != 0 {
		t.Fatalf("empty presence=%v error=%v", found, err)
	}
	if engine.views != before {
		t.Fatalf("empty presence snapshots=%d, want=%d", engine.views, before)
	}
}

func TestDocumentSetPresenceRejectsCancellationInsideSnapshot(t *testing.T) {
	directory, receiver, _ := openScriptedDocuments(t)
	url := "https://example.org/present"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: url}}); err != nil {
		t.Fatal(err)
	}
	ctx := &errAfterContext{
		Context:   context.Background(),
		remaining: 3,
		err:       context.Canceled,
	}
	if _, err := directory.(DocumentSetPresence).DocumentsExist(
		ctx,
		[]string{url, url},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("presence cancellation=%v", err)
	}
}

func TestDocumentSetPresenceRejectsMalformedLocation(t *testing.T) {
	directory, _, engine := openScriptedDocuments(t)
	url := "https://example.org/malformed-location"
	engine.buckets[documentLocationBucketName][url] = []byte{1}
	if _, err := directory.(DocumentSetPresence).DocumentsExist(
		t.Context(),
		[]string{url},
	); err == nil {
		t.Fatal("malformed document location accepted")
	}
}
