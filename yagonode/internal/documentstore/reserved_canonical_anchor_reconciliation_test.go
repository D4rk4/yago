package documentstore

import (
	"fmt"
	"testing"
)

func TestCanonicalDocumentsReconcileCurrentAnchorsAtReceive(t *testing.T) {
	t.Run("reserved", func(t *testing.T) {
		testCanonicalDocumentsReconcileCurrentAnchorsAtReceive(t, true)
	})
	t.Run("ordinary", func(t *testing.T) {
		testCanonicalDocumentsReconcileCurrentAnchorsAtReceive(t, false)
	})
}

func TestStoredDocumentRoundTripReconcilesCurrentAnchorsAtReceive(t *testing.T) {
	directory, receiver := openDocuments(t)
	targetURL := "https://target.example/stored-round-trip"
	sourceURL := "https://source.example/stored-round-trip"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: targetURL}}); err != nil {
		t.Fatal(err)
	}
	replacer := receiver.(OutboundAnchorDocumentReplacer)
	if _, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: sourceURL,
			Anchors: []OutboundAnchor{{
				TargetURL: targetURL,
				Text:      "previous",
			}},
		}},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	snapshot, found, err := directory.(DocumentRevisionDirectory).DocumentRevision(
		t.Context(),
		targetURL,
	)
	if err != nil || !found || len(snapshot.Inlinks) != 1 ||
		snapshot.Inlinks[0].Text != "previous" {
		t.Fatalf("stored anchor snapshot = %#v/%t/%v", snapshot, found, err)
	}
	snapshot.ClusterID = "cluster"
	if _, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: sourceURL,
			Anchors: []OutboundAnchor{{
				TargetURL: targetURL,
				Text:      "current",
			}},
		}},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	receipt, err := receiver.Receive(t.Context(), []Document{snapshot})
	if err != nil || len(receipt.CommittedDocuments) != 1 {
		t.Fatalf("stored round-trip receipt = %#v/%v", receipt, err)
	}
	committed := receipt.CommittedDocuments[0]
	if committed.ClusterID != "cluster" || len(committed.Inlinks) != 1 ||
		committed.Inlinks[0].URL != sourceURL || committed.Inlinks[0].Text != "current" {
		t.Fatalf("stored round-trip document = %#v", committed)
	}
}

func TestStoredDocumentRoundTripRetainsLegacyInlinksWithoutAnchorRow(t *testing.T) {
	directory, receiver := openDocuments(t)
	targetURL := "https://target.example/legacy-round-trip"
	legacy := AnchorText{URL: "https://legacy.example/", Text: "legacy"}
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: targetURL,
		Inlinks:       []AnchorText{legacy},
	}}); err != nil {
		t.Fatal(err)
	}
	snapshot, found, err := directory.(DocumentRevisionDirectory).DocumentRevision(
		t.Context(),
		targetURL,
	)
	if err != nil || !found {
		t.Fatalf("legacy anchor snapshot = %#v/%t/%v", snapshot, found, err)
	}
	snapshot.ClusterID = "legacy-cluster"
	receipt, err := receiver.Receive(t.Context(), []Document{snapshot})
	if err != nil || len(receipt.CommittedDocuments) != 1 {
		t.Fatalf("legacy round-trip receipt = %#v/%v", receipt, err)
	}
	committed := receipt.CommittedDocuments[0]
	if committed.ClusterID != "legacy-cluster" || len(committed.Inlinks) != 1 ||
		committed.Inlinks[0] != legacy {
		t.Fatalf("legacy round-trip document = %#v", committed)
	}
}

func TestCanonicalDocumentBoundsSubmittedInlinksAfterAnchorRowDeletion(t *testing.T) {
	directory, receiver := openDocuments(t)
	targetURL := "https://target.example/bounded-submitted"
	sourceURL := "https://source.example/bounded-submitted"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: targetURL}}); err != nil {
		t.Fatal(err)
	}
	replacer := receiver.(OutboundAnchorDocumentReplacer)
	if _, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: sourceURL,
			Anchors: []OutboundAnchor{{
				TargetURL: targetURL,
				Text:      "materialized",
			}},
		}},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	inlinks := make([]AnchorText, 0, maximumInboundAnchors+16)
	for index := range maximumInboundAnchors + 16 {
		inlinks = append(inlinks, AnchorText{
			URL:  "https://submitted.example/" + fmt.Sprint(index),
			Text: "submitted",
		})
	}
	canonical, err := receiver.(CanonicalDocumentDirectory).CanonicalDocuments(
		t.Context(),
		[]Document{{NormalizedURL: targetURL, Inlinks: inlinks}},
	)
	if err != nil || len(canonical) != 1 ||
		len(canonical[0].submittedInlinks) != maximumInboundAnchors {
		t.Fatalf("bounded canonical provenance = %#v/%v", canonical, err)
	}
	if _, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{SourceURL: sourceURL}},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Receive(t.Context(), canonical); err != nil {
		t.Fatal(err)
	}
	stored, found, err := directory.Document(t.Context(), targetURL)
	if err != nil || !found || len(stored.Inlinks) != maximumInboundAnchors {
		t.Fatalf("bounded stored provenance = %d/%t/%v", len(stored.Inlinks), found, err)
	}
}

func testCanonicalDocumentsReconcileCurrentAnchorsAtReceive(t *testing.T, reserved bool) {
	t.Helper()
	directory, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	targetURL := "https://target.example/reconciled"
	sourceURL := "https://source.example/reconciled"
	submittedURL := "https://submitted.example/reconciled"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: targetURL}}); err != nil {
		t.Fatal(err)
	}
	replacer := receiver.(OutboundAnchorDocumentReplacer)
	if _, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: sourceURL,
			Anchors: []OutboundAnchor{{
				TargetURL: targetURL,
				Text:      "previous",
			}},
		}},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	incoming := []Document{{
		NormalizedURL: targetURL,
		Inlinks: []AnchorText{{
			URL:  submittedURL,
			Text: "submitted",
		}},
	}}
	var canonical []Document
	var reservation DocumentLineageReservation
	var err error
	if reserved {
		reservation, err = documents.ReserveDocumentLineages(t.Context(), []string{targetURL})
		if err != nil {
			t.Fatal(err)
		}
		defer documents.ReleaseDocumentLineages(reservation)
		canonical, err = documents.CanonicalReservedDocuments(
			t.Context(),
			reservation,
			incoming,
		)
	} else {
		canonical, err = documents.CanonicalDocuments(t.Context(), incoming)
	}
	if err != nil || len(canonical) != 1 ||
		!documentHasInboundAnchorSource(canonical[0], sourceURL) ||
		!documentHasInboundAnchorSource(canonical[0], submittedURL) {
		t.Fatalf("reserved canonical document = %#v/%v", canonical, err)
	}
	if _, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: sourceURL,
			Anchors: []OutboundAnchor{{
				TargetURL: targetURL,
				Text:      "current",
			}},
		}},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	receipt, err := receiver.Receive(t.Context(), canonical)
	if err != nil || len(receipt.CommittedDocuments) != 1 {
		t.Fatalf("receive reconciled document = %#v/%v", receipt, err)
	}
	assertReconciledInboundAnchors(
		t,
		receipt.CommittedDocuments[0],
		sourceURL,
		submittedURL,
	)
	stored, found, err := directory.Document(t.Context(), targetURL)
	if err != nil || !found {
		t.Fatalf("stored reconciled document = %#v/%t/%v", stored, found, err)
	}
	assertReconciledInboundAnchors(t, stored, sourceURL, submittedURL)
}

func assertReconciledInboundAnchors(
	t *testing.T,
	document Document,
	sourceURL string,
	submittedURL string,
) {
	t.Helper()
	if len(document.Inlinks) != 2 {
		t.Fatalf("reconciled inbound anchors = %#v", document.Inlinks)
	}
	texts := make(map[string]string, len(document.Inlinks))
	for _, anchor := range document.Inlinks {
		texts[anchor.URL] = anchor.Text
	}
	if texts[sourceURL] != "current" || texts[submittedURL] != "submitted" {
		t.Fatalf("reconciled inbound anchors = %#v", document.Inlinks)
	}
}
