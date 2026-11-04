package documentstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOutboundAnchorDocumentReplacementRejectsPublicationBudgetBeforeMutation(
	t *testing.T,
) {
	_, documents, engine := openDocumentStorageFaultVault(t)
	sets, err := canonicalOutboundAnchorSets(outboundAnchorReplacementSet("publication-budget"))
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := documents.ReserveDocumentLineages(
		t.Context(),
		[]string{sets[0].SourceURL},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer documents.ReleaseDocumentLineages(reservation)
	if _, err := documents.replaceReservedOutboundAnchorDocumentsWithin(
		t.Context(),
		reservation,
		sets,
		1,
	); err == nil {
		t.Fatal("undersized publication budget was accepted")
	}
	if len(engine.base.buckets[inboundAnchorBucket]) != 0 {
		t.Fatal("publication budget failure mutated target anchors")
	}
}

func TestReservedOutboundAnchorDocumentReplacementRequiresLineageThroughPublication(
	t *testing.T,
) {
	_, receiver, engine := openOutboundAnchorObservedDocuments(t)
	documents := receiver.(documentVault)
	sourceURL := "https://source.example/released-before-publication"
	targetURL := "https://target.example/released-before-publication"
	if _, err := receiver.Receive(
		t.Context(),
		[]Document{{NormalizedURL: targetURL}},
	); err != nil {
		t.Fatal(err)
	}
	reservation, err := documents.ReserveDocumentLineages(t.Context(), []string{sourceURL})
	if err != nil {
		t.Fatal(err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			documents.ReleaseDocumentLineages(reservation)
		}
	})
	engine.observeAfterUpdate(func(observation outboundAnchorTransactionObservation) {
		if _, mutated := observation.buckets[inboundAnchorBucket]; mutated && !released {
			documents.ReleaseDocumentLineages(reservation)
			released = true
		}
	})
	_, err = documents.ReplaceReservedOutboundAnchorDocuments(
		t.Context(),
		reservation,
		[]OutboundAnchorSet{{
			SourceURL: sourceURL,
			Anchors:   outboundAnchorReplacementEdges([]string{targetURL}),
		}},
	)
	if err == nil || !released {
		t.Fatalf("replacement after source lineage release = %t/%v", released, err)
	}
	publication := readOutboundAnchorReplacementPublication(t, documents, sourceURL)
	if len(publication.Targets) != 0 {
		t.Fatalf("released-lineage publication = %#v", publication)
	}
}

func TestOutboundAnchorTargetPreparationSurfacesStoredRowFailures(t *testing.T) {
	t.Run("document location", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		targetURL := "https://target.example/corrupt-location"
		engine.putRaw(documentLocationBucketName, vault.Key(targetURL), []byte{1})
		_, err := documents.ReplaceOutboundAnchorDocuments(
			t.Context(),
			outboundAnchorErrorSet("location", targetURL),
		)
		if err == nil {
			t.Fatal("corrupt document location was accepted")
		}
	})
	t.Run("inbound anchors", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		targetURL := "https://target.example/corrupt-inbound"
		engine.putRaw(inboundAnchorBucket, vault.Key(targetURL), []byte("{"))
		_, err := documents.ReplaceOutboundAnchorDocuments(
			t.Context(),
			outboundAnchorErrorSet("inbound", targetURL),
		)
		if err == nil {
			t.Fatal("corrupt inbound anchors were accepted")
		}
	})
}

func TestOutboundAnchorTargetMutationSurfacesDeleteAndDocumentFailures(t *testing.T) {
	t.Run("delete", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		sourceURL := "https://source.example/delete-failure"
		targetURL := "https://target.example/delete-failure"
		if _, err := documents.ReplaceOutboundAnchorDocuments(
			t.Context(),
			outboundAnchorErrorSet("delete-failure", targetURL),
		); err != nil {
			t.Fatal(err)
		}
		want := errors.New("delete target failed")
		engine.deleteErrors[inboundAnchorBucket] = want
		_, err := documents.ReplaceOutboundAnchorDocuments(
			t.Context(),
			[]OutboundAnchorSet{{SourceURL: sourceURL}},
		)
		if !errors.Is(err, want) {
			t.Fatalf("target delete failure = %v", err)
		}
	})
	t.Run("document", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		targetURL := "https://target.example/document-failure"
		if _, err := documents.Receive(
			t.Context(),
			[]Document{{NormalizedURL: targetURL}},
		); err != nil {
			t.Fatal(err)
		}
		if _, err := documents.ReplaceOutboundAnchorDocuments(
			t.Context(),
			outboundAnchorErrorSet("document-failure", targetURL),
		); err != nil {
			t.Fatal(err)
		}
		want := errors.New("target document failed")
		engine.putErrors[orderedDocumentBucketName] = want
		if _, err := documents.RefreshInboundAnchorDocuments(
			t.Context(),
			outboundAnchorTargetPageSize,
			func([]Document) error { return nil },
		); !errors.Is(err, want) {
			t.Fatalf("target document failure = %v", err)
		}
	})
}

func TestInboundAnchorRefreshSurfacesMarkAndClearFailures(t *testing.T) {
	t.Run("refresh mark", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		targetURL := "https://target.example/refresh-mark-failure"
		if _, err := documents.Receive(
			t.Context(),
			[]Document{{NormalizedURL: targetURL}},
		); err != nil {
			t.Fatal(err)
		}
		want := errors.New("refresh mark failed")
		engine.putErrors[inboundAnchorRefreshBucket] = want
		if _, err := documents.ReplaceOutboundAnchorDocuments(
			t.Context(),
			outboundAnchorErrorSet("refresh-mark-failure", targetURL),
		); !errors.Is(err, want) {
			t.Fatalf("refresh mark failure = %v", err)
		}
	})
	t.Run("refresh clear", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		targetURL := "https://target.example/refresh-clear-failure"
		if _, err := documents.Receive(
			t.Context(),
			[]Document{{NormalizedURL: targetURL}},
		); err != nil {
			t.Fatal(err)
		}
		if _, err := documents.ReplaceOutboundAnchorDocuments(
			t.Context(),
			outboundAnchorErrorSet("refresh-clear-failure", targetURL),
		); err != nil {
			t.Fatal(err)
		}
		want := errors.New("refresh clear failed")
		engine.deleteErrors[inboundAnchorRefreshBucket] = want
		if _, err := documents.RefreshInboundAnchorDocuments(
			t.Context(),
			outboundAnchorTargetPageSize,
			func([]Document) error { return nil },
		); !errors.Is(err, want) {
			t.Fatalf("refresh clear failure = %v", err)
		}
	})
}

func TestOutboundAnchorTargetPageSurfacesAdmissionAndLockFailures(t *testing.T) {
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	replacement := outboundAnchorDocumentReplacement{}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := documents.replaceOutboundAnchorDocumentTargetPage(
		canceled,
		replacement,
		[]string{"https://target.example/canceled-write"},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled write admission = %v", err)
	}
	targetURL := "https://target.example/contended-page"
	release, err := documents.urlBoundaries.lockWrites(t.Context(), []string{targetURL})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	deadline, deadlineCancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer deadlineCancel()
	if err := documents.replaceOutboundAnchorDocumentTargetPage(
		deadline,
		replacement,
		[]string{targetURL},
	); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended target page = %v", err)
	}
}

func TestInboundAnchorRefreshStoresOversizedDocumentInItsOwnTransaction(t *testing.T) {
	directory, receiver := openDocuments(t)
	oversized := "https://target.example/oversized-mutation"
	ordinary := "https://target.example/ordinary-mutation"
	if _, err := receiver.Receive(t.Context(), []Document{
		{
			NormalizedURL: oversized,
			Title:         strings.Repeat("x", outboundAnchorMutationMaximumEncodedBytes+1),
		},
		{NormalizedURL: ordinary},
	}); err != nil {
		t.Fatal(err)
	}
	sourceURL := "https://source.example/oversized-mutation"
	if _, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: sourceURL,
			Anchors:   outboundAnchorReplacementEdges([]string{oversized, ordinary}),
		}},
	); err != nil {
		t.Fatal(err)
	}
	drainInboundAnchorRefreshes(t, receiver)
	for _, targetURL := range []string{oversized, ordinary} {
		stored, found, err := directory.Document(t.Context(), targetURL)
		if err != nil || !found || !documentHasInboundAnchorSource(stored, sourceURL) {
			t.Fatalf("refreshed %s = %t/%v", targetURL, found, err)
		}
	}
}

func outboundAnchorErrorSet(identity string, targetURL string) []OutboundAnchorSet {
	return []OutboundAnchorSet{{
		SourceURL: "https://source.example/" + identity,
		Anchors: []OutboundAnchor{{
			TargetURL: targetURL,
			Text:      identity,
		}},
	}}
}
