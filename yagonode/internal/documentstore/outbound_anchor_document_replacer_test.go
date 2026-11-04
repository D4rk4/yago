package documentstore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestInboundAnchorRefreshLocksCurrentTargetPageOnly(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
	targets := outboundAnchorReplacementTargets(outboundAnchorTargetPageSize + 2)
	unrelated := "https://target.example/unrelated"
	documents := make([]Document, 0, len(targets)+1)
	for _, target := range append(targets, unrelated) {
		documents = append(documents, Document{NormalizedURL: target, Title: target})
	}
	if _, err := receiver.Receive(t.Context(), documents); err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: "https://source.example/page",
			Anchors:   outboundAnchorReplacementEdges(targets),
		}},
	); err != nil {
		t.Fatal(err)
	}
	firstPageEntered := make(chan struct{})
	releaseFirstPage := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := receiver.(InboundAnchorDocumentRefresher).RefreshInboundAnchorDocuments(
			t.Context(),
			outboundAnchorTargetPageSize,
			func([]Document) error {
				close(firstPageEntered)
				<-releaseFirstPage

				return nil
			},
		)
		result <- err
	}()
	select {
	case <-firstPageEntered:
	case <-time.After(time.Second):
		t.Fatal("first refresh page was not reindexed")
	}
	currentRead := make(chan error, 1)
	go func() {
		_, _, err := directory.Document(t.Context(), targets[0])
		currentRead <- err
	}()
	select {
	case err := <-currentRead:
		t.Fatalf("current-page read crossed the reindex callback: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	assertPromptStoredDocument(t, directory, targets[outboundAnchorTargetPageSize+1])
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
		t.Fatal("inbound anchor refresh did not finish")
	}
}

func TestOutboundAnchorDocumentReplacementBoundsStorageMutationByProjectionPage(
	t *testing.T,
) {
	for _, page := range []struct {
		targets   int
		mutations int
	}{
		{targets: outboundAnchorTargetPageSize, mutations: 1},
		{targets: outboundAnchorTargetPageSize + 1, mutations: 2},
	} {
		_, receiver, engine := openPagedDocuments(t)
		targets := outboundAnchorReplacementTargets(page.targets)
		stored := make([]Document, 0, len(targets))
		for _, target := range targets {
			stored = append(stored, Document{NormalizedURL: target})
		}
		if _, err := receiver.Receive(t.Context(), stored); err != nil {
			t.Fatal(err)
		}
		updates := 0
		engine.beforeUpdate = func() { updates++ }
		if _, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
			t.Context(),
			[]OutboundAnchorSet{{
				SourceURL: "https://source.example/coalesced",
				Anchors:   outboundAnchorReplacementEdges(targets),
			}},
		); err != nil {
			t.Fatal(err)
		}
		if updates != page.mutations+1 {
			t.Fatalf(
				"%d targets: storage transactions = %d, want %d mutations and one publication",
				page.targets,
				updates,
				page.mutations,
			)
		}
	}
}

func TestInboundAnchorRefreshRetriesAfterReindexFailure(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
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
	refresher := receiver.(InboundAnchorDocumentRefresher)
	for _, edges := range [][]string{oldTargets, newTargets} {
		if _, err := replacer.ReplaceOutboundAnchorDocuments(
			t.Context(),
			[]OutboundAnchorSet{{
				SourceURL: source,
				Anchors:   outboundAnchorReplacementEdges(edges),
			}},
		); err != nil {
			t.Fatal(err)
		}
	}
	wantFailure := errors.New("reindex failed")
	_, err := refresher.RefreshInboundAnchorDocuments(
		t.Context(),
		len(targets),
		func([]Document) error { return wantFailure },
	)
	if !errors.Is(err, wantFailure) {
		t.Fatalf("reindex failure = %v", err)
	}
	unrefreshed, found, err := directory.Document(t.Context(), targets[29])
	if err != nil || !found || documentHasInboundAnchorSource(unrefreshed, source) {
		t.Fatalf("target after failed reindex = %#v/%t/%v", unrefreshed, found, err)
	}
	reindexed := 0
	receipt, err := refresher.RefreshInboundAnchorDocuments(
		t.Context(),
		len(targets),
		func(documents []Document) error {
			reindexed += len(documents)

			return nil
		},
	)
	if err != nil || receipt.More || receipt.Refreshed != len(newTargets) ||
		reindexed != len(newTargets) {
		t.Fatalf("refresh retry = %#v, reindexed=%d, error=%v", receipt, reindexed, err)
	}
	removed, found, err := directory.Document(t.Context(), targets[0])
	if err != nil || !found || documentHasInboundAnchorSource(removed, source) {
		t.Fatalf("removed target after retry = %#v/%t/%v", removed, found, err)
	}
	added, found, err := directory.Document(t.Context(), targets[29])
	if err != nil || !found || !documentHasInboundAnchorSource(added, source) {
		t.Fatalf("added target after retry = %#v/%t/%v", added, found, err)
	}
}

func TestOutboundAnchorDocumentReplacementBusyDoesNotMutate(t *testing.T) {
	vaulted, directory, receiver := openDocumentsWithVault(t, 1)
	target := "https://target.example/capacity"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: target}}); err != nil {
		t.Fatal(err)
	}
	if _, err := vaulted.UsedBytes(t.Context()); err != nil {
		t.Fatal(err)
	}
	receipt, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: "https://source.example/capacity",
			Anchors:   outboundAnchorReplacementEdges([]string{target}),
		}},
	)
	if err != nil || !receipt.Busy {
		t.Fatalf("capacity replacement = %#v, error=%v", receipt, err)
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
