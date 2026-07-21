package documentstore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestDocumentRevisionDirectoryMissingCanceledAndFaults(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		directory, _ := openDocuments(t)
		document, found, err := directory.(DocumentRevisionDirectory).DocumentRevision(
			t.Context(),
			"https://missing.example/revision",
		)
		if err != nil || found || document.NormalizedURL != "" {
			t.Fatalf("missing revision = %#v/%t/%v", document, found, err)
		}
	})
	t.Run("canceled", func(t *testing.T) {
		directory, _ := openDocuments(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, _, err := directory.(DocumentRevisionDirectory).DocumentRevision(
			ctx,
			"https://canceled.example/revision",
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled revision = %v", err)
		}
	})
	t.Run("view", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		want := errors.New("revision view failed")
		engine.viewError = want
		_, _, err := documents.DocumentRevision(
			t.Context(),
			"https://view.example/revision",
		)
		if !errors.Is(err, want) {
			t.Fatalf("view revision = %v", err)
		}
	})
	t.Run("anchors", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		url := "https://anchors.example/revision"
		if _, err := documents.Receive(
			t.Context(),
			[]Document{{NormalizedURL: url}},
		); err != nil {
			t.Fatal(err)
		}
		engine.putRaw(inboundAnchorBucket, vault.Key(url), []byte("{"))
		if _, _, err := documents.DocumentRevision(t.Context(), url); err == nil {
			t.Fatal("malformed revision anchors were accepted")
		}
	})
}

func TestDocumentRevisionBoundsLegacyInboundEvidence(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	url := "https://legacy.example/oversized-inbound-evidence"
	if _, err := receiver.Receive(
		t.Context(),
		[]Document{{NormalizedURL: url}},
	); err != nil {
		t.Fatal(err)
	}
	inlinks := make([]AnchorText, 0, maximumInboundAnchors+16)
	for index := range maximumInboundAnchors + 16 {
		inlinks = append(inlinks, AnchorText{
			URL:  "https://legacy-source.example/" + fmt.Sprint(index),
			Text: "legacy",
		})
	}
	raw, err := (documentCodec{}).Encode(Document{
		CanonicalURL:  url,
		NormalizedURL: url,
		Inlinks:       inlinks,
	})
	if err != nil {
		t.Fatal(err)
	}
	key := scriptedOrderedDocumentKey(t, engine, url)
	engine.buckets[orderedDocumentBucketName][key] = raw
	revision, found, err := directory.(DocumentRevisionDirectory).DocumentRevision(
		t.Context(),
		url,
	)
	if err != nil || !found || len(revision.Inlinks) != maximumInboundAnchors ||
		len(revision.submittedInlinks) != maximumInboundAnchors {
		t.Fatalf("bounded legacy revision = %#v/%t/%v", revision, found, err)
	}
	if _, err := receiver.Receive(t.Context(), []Document{revision}); err != nil {
		t.Fatal(err)
	}
	stored, found, err := directory.Document(t.Context(), url)
	if err != nil || !found || len(stored.Inlinks) != maximumInboundAnchors {
		t.Fatalf("bounded legacy stored evidence = %d/%t/%v", len(stored.Inlinks), found, err)
	}
}
