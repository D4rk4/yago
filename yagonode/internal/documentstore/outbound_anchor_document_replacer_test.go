package documentstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOutboundAnchorDocumentReplacementLocksCurrentTargetPageOnly(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
	targets := outboundAnchorReplacementTargets(34)
	unrelated := "https://target.example/unrelated"
	documents := make([]Document, 0, len(targets)+1)
	for _, target := range append(targets, unrelated) {
		documents = append(documents, Document{NormalizedURL: target, Title: target})
	}
	if _, err := receiver.Receive(t.Context(), documents); err != nil {
		t.Fatal(err)
	}
	firstPageEntered := make(chan struct{})
	releaseFirstPage := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
			t.Context(),
			[]OutboundAnchorSet{{
				SourceURL: "https://source.example/page",
				Anchors:   outboundAnchorReplacementEdges(targets),
			}},
			func([]Document) error {
				select {
				case <-firstPageEntered:
				default:
					close(firstPageEntered)
					<-releaseFirstPage
				}

				return nil
			},
		)
		result <- err
	}()
	select {
	case <-firstPageEntered:
	case <-time.After(time.Second):
		t.Fatal("first target page was not visited")
	}
	currentRead := make(chan error, 1)
	go func() {
		_, _, err := directory.Document(t.Context(), targets[0])
		currentRead <- err
	}()
	select {
	case err := <-currentRead:
		t.Fatalf("current-page read crossed index callback: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	assertPromptStoredDocument(t, directory, targets[20])
	assertPromptStoredDocument(t, directory, unrelated)
	close(releaseFirstPage)
	select {
	case err := <-currentRead:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("current-page read remained blocked")
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("paged anchor replacement did not finish")
	}
}

func TestOutboundAnchorDocumentReplacementBoundsStorageMutationByProjectionPage(
	t *testing.T,
) {
	_, receiver, engine := openPagedDocuments(t)
	targets := outboundAnchorReplacementTargets(34)
	stored := make([]Document, 0, len(targets))
	for _, target := range targets {
		stored = append(stored, Document{NormalizedURL: target})
	}
	if _, err := receiver.Receive(t.Context(), stored); err != nil {
		t.Fatal(err)
	}
	updates := 0
	engine.beforeUpdate = func() { updates++ }
	visits := 0
	if _, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: "https://source.example/coalesced",
			Anchors:   outboundAnchorReplacementEdges(targets),
		}},
		func([]Document) error {
			visits++

			return nil
		},
	); err != nil {
		t.Fatal(err)
	}
	if updates != 4 {
		t.Fatalf("storage transactions = %d, want 3 mutations and one publication", updates)
	}
	if visits != 3 {
		t.Fatalf("projection visits = %d, want 3 bounded pages", visits)
	}
}

func TestOutboundAnchorDocumentReplacementReplayConvergesAfterVisitFailure(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
	documents := receiver.(documentVault)
	targets := outboundAnchorReplacementTargets(30)
	stored := make([]Document, 0, len(targets))
	for _, target := range targets {
		stored = append(stored, Document{NormalizedURL: target})
	}
	if _, err := receiver.Receive(t.Context(), stored); err != nil {
		t.Fatal(err)
	}
	source := "https://source.example/replay"
	oldTargets := targets[:20]
	newTargets := targets[10:]
	replacer := receiver.(OutboundAnchorDocumentReplacer)
	if _, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: source,
			Anchors:   outboundAnchorReplacementEdges(oldTargets),
		}},
		func([]Document) error { return nil },
	); err != nil {
		t.Fatal(err)
	}
	wantFailure := errors.New("projection failed")
	visits := 0
	_, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: source,
			Anchors:   outboundAnchorReplacementEdges(newTargets),
		}},
		func([]Document) error {
			visits++
			if visits == 2 {
				return wantFailure
			}

			return nil
		},
	)
	if !errors.Is(err, wantFailure) {
		t.Fatalf("visit failure = %v", err)
	}
	publication := readOutboundAnchorReplacementPublication(t, documents, source)
	if !slices.Equal(publication.Targets, oldTargets) {
		t.Fatalf("publication after failure = %#v", publication.Targets)
	}
	assertPromptStoredDocument(t, directory, targets[16])
	if _, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: source,
			Anchors:   outboundAnchorReplacementEdges(newTargets),
		}},
		func([]Document) error { return nil },
	); err != nil {
		t.Fatalf("replay: %v", err)
	}
	publication = readOutboundAnchorReplacementPublication(t, documents, source)
	if !slices.Equal(publication.Targets, newTargets) {
		t.Fatalf("publication after replay = %#v", publication.Targets)
	}
	removed, found, err := directory.Document(t.Context(), targets[0])
	if err != nil || !found || documentHasInboundAnchorSource(removed, source) {
		t.Fatalf("removed target after replay = %#v/%t/%v", removed, found, err)
	}
	added, found, err := directory.Document(t.Context(), targets[29])
	if err != nil || !found || !documentHasInboundAnchorSource(added, source) {
		t.Fatalf("added target after replay = %#v/%t/%v", added, found, err)
	}
}

func TestOutboundAnchorDocumentReplacementBusyDoesNotMutateOrVisit(t *testing.T) {
	vaulted, directory, receiver := openDocumentsWithVault(t, 1)
	target := "https://target.example/capacity"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: target}}); err != nil {
		t.Fatal(err)
	}
	if _, err := vaulted.UsedBytes(t.Context()); err != nil {
		t.Fatal(err)
	}
	visits := 0
	receipt, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: "https://source.example/capacity",
			Anchors:   outboundAnchorReplacementEdges([]string{target}),
		}},
		func([]Document) error {
			visits++

			return nil
		},
	)
	if err != nil || !receipt.Busy || visits != 0 {
		t.Fatalf("capacity replacement = %#v, visits=%d, error=%v", receipt, visits, err)
	}
	stored, found, err := directory.Document(t.Context(), target)
	if err != nil || !found || len(stored.Inlinks) != 0 {
		t.Fatalf("capacity target = %#v/%t/%v", stored, found, err)
	}
}

func outboundAnchorReplacementTargets(total int) []string {
	targets := make([]string, 0, total)
	for sequence := range total {
		targets = append(
			targets,
			fmt.Sprintf("https://target.example/%03d", sequence),
		)
	}

	return targets
}

func outboundAnchorReplacementEdges(targets []string) []OutboundAnchor {
	edges := make([]OutboundAnchor, 0, len(targets))
	for _, target := range targets {
		edges = append(edges, OutboundAnchor{TargetURL: target, Text: target})
	}

	return edges
}

func assertPromptStoredDocument(
	t *testing.T,
	directory DocumentDirectory,
	normalizedURL string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, found, err := directory.Document(ctx, normalizedURL)
	if err != nil || !found {
		t.Fatalf("prompt document %s = %t/%v", normalizedURL, found, err)
	}
}

func readOutboundAnchorReplacementPublication(
	t *testing.T,
	documents documentVault,
	sourceURL string,
) outboundAnchorPublication {
	t.Helper()
	var publication outboundAnchorPublication
	err := documents.vault.View(t.Context(), func(tx *vault.Txn) error {
		read, err := documents.readOutboundAnchorPublication(tx, sourceURL)
		publication = read

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return publication
}

func documentHasInboundAnchorSource(document Document, sourceURL string) bool {
	for _, anchor := range document.Inlinks {
		if anchor.URL == sourceURL {
			return true
		}
	}

	return false
}
