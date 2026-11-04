package documentstore

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func drainInboundAnchorRefreshes(t *testing.T, receiver DocumentReceiver) []Document {
	t.Helper()
	refresher := receiver.(InboundAnchorDocumentRefresher)
	reindexed := make([]Document, 0)
	for {
		receipt, err := refresher.RefreshInboundAnchorDocuments(
			t.Context(),
			outboundAnchorTargetPageSize,
			func(documents []Document) error {
				reindexed = append(reindexed, documents...)

				return nil
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if !receipt.More {
			return reindexed
		}
	}
}

func pendingInboundAnchorRefresh(
	t *testing.T,
	receiver DocumentReceiver,
	targetURL string,
) (inboundAnchorRefresh, bool) {
	t.Helper()
	documents := receiver.(documentVault)
	var refresh inboundAnchorRefresh
	var found bool
	if err := documents.vault.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		refresh, found, err = documents.inboundAnchorRefreshes.Get(tx, vault.Key(targetURL))
		if err != nil {
			return fmt.Errorf("read pending inbound anchor refresh: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return refresh, found
}

func replaceOutboundAnchorText(
	t *testing.T,
	receiver DocumentReceiver,
	sourceURL string,
	targetURL string,
	text string,
) {
	t.Helper()
	if _, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: sourceURL,
			Anchors:   []OutboundAnchor{{TargetURL: targetURL, Text: text}},
		}},
	); err != nil {
		t.Fatal(err)
	}
}

func TestOutboundAnchorReplacementDefersStoredTargetRefresh(t *testing.T) {
	directory, receiver := openDocuments(t)
	target := "https://target.example/deferred"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: target}}); err != nil {
		t.Fatal(err)
	}
	replaceOutboundAnchorText(t, receiver, "https://source.example/deferred", target, "deferred")

	stored, found, err := directory.Document(t.Context(), target)
	if err != nil || !found || len(stored.Inlinks) != 0 {
		t.Fatalf("target before refresh = %#v/%t/%v", stored.Inlinks, found, err)
	}
	refresh, pending := pendingInboundAnchorRefresh(t, receiver, target)
	if !pending ||
		!slices.Equal(refresh.ReplacedSources, []string{"https://source.example/deferred"}) {
		t.Fatalf("pending refresh = %#v/%t", refresh, pending)
	}
	reindexed := drainInboundAnchorRefreshes(t, receiver)
	if len(reindexed) != 1 || !documentHasInboundAnchorSource(
		reindexed[0],
		"https://source.example/deferred",
	) {
		t.Fatalf("reindexed refresh = %#v", reindexed)
	}
	stored, found, err = directory.Document(t.Context(), target)
	if err != nil || !found ||
		!documentHasInboundAnchorSource(stored, "https://source.example/deferred") {
		t.Fatalf("target after refresh = %#v/%t/%v", stored.Inlinks, found, err)
	}
	if _, pending := pendingInboundAnchorRefresh(t, receiver, target); pending {
		t.Fatal("refresh mark survived a completed refresh")
	}
}

func TestOutboundAnchorReplacementMarksOnlyStoredTargetsWhoseInlinksChange(t *testing.T) {
	_, receiver := openDocuments(t)
	stored := "https://target.example/stored"
	missing := "https://target.example/missing"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: stored}}); err != nil {
		t.Fatal(err)
	}
	source := "https://source.example/marks"
	if _, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: source,
			Anchors: []OutboundAnchor{
				{TargetURL: stored, Text: "stored"},
				{TargetURL: missing, Text: "missing"},
			},
		}},
	); err != nil {
		t.Fatal(err)
	}
	if _, pending := pendingInboundAnchorRefresh(t, receiver, missing); pending {
		t.Fatal("target without a stored document was marked for refresh")
	}
	if _, pending := pendingInboundAnchorRefresh(t, receiver, stored); !pending {
		t.Fatal("stored target whose inlinks change was not marked for refresh")
	}
	drainInboundAnchorRefreshes(t, receiver)

	replaceOutboundAnchorText(t, receiver, source, stored, "stored")
	if _, pending := pendingInboundAnchorRefresh(t, receiver, stored); pending {
		t.Fatal("unchanged anchor replacement marked an already-refreshed target")
	}
}

func TestInboundAnchorRefreshCoalescesSourcesIntoOneReindex(t *testing.T) {
	directory, receiver := openDocuments(t)
	target := "https://target.example/coalesced"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: target}}); err != nil {
		t.Fatal(err)
	}
	sources := []string{
		"https://source.example/coalesced/a",
		"https://source.example/coalesced/b",
		"https://source.example/coalesced/c",
	}
	for _, source := range sources {
		replaceOutboundAnchorText(t, receiver, source, target, source)
	}
	refresh, _ := pendingInboundAnchorRefresh(t, receiver, target)
	if !slices.Equal(refresh.ReplacedSources, sources) {
		t.Fatalf("coalesced refresh sources = %#v", refresh.ReplacedSources)
	}

	reindexed := drainInboundAnchorRefreshes(t, receiver)
	if len(reindexed) != 1 || len(reindexed[0].Inlinks) != len(sources) {
		t.Fatalf("coalesced reindex = %#v", reindexed)
	}
	if again := drainInboundAnchorRefreshes(t, receiver); len(again) != 0 {
		t.Fatalf("drained refresh reindexed again: %#v", again)
	}
	stored, _, err := directory.Document(t.Context(), target)
	if err != nil || len(stored.Inlinks) != len(sources) {
		t.Fatalf("coalesced target = %#v/%v", stored.Inlinks, err)
	}
}

func TestInboundAnchorRefreshReplacesStaleSourceAnchorsAndKeepsOthers(t *testing.T) {
	directory, receiver := openDocuments(t)
	target := "https://target.example/stale"
	kept := AnchorText{URL: "https://kept.example/", Text: "kept"}
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: target,
		Inlinks:       []AnchorText{kept},
	}}); err != nil {
		t.Fatal(err)
	}
	source := "https://source.example/stale"
	replaceOutboundAnchorText(t, receiver, source, target, "old text")
	drainInboundAnchorRefreshes(t, receiver)
	replaceOutboundAnchorText(t, receiver, source, target, "new text")
	drainInboundAnchorRefreshes(t, receiver)

	stored, _, err := directory.Document(t.Context(), target)
	want := []AnchorText{kept, {URL: source, Text: "new text"}}
	if err != nil || !slices.Equal(stored.Inlinks, want) {
		t.Fatalf("refreshed inlinks = %#v/%v, want %#v", stored.Inlinks, err, want)
	}
}

func TestInboundAnchorRefreshClearsMarkOfMissingOrUnchangedDocument(t *testing.T) {
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	missing := "https://target.example/gone"
	unchanged := "https://target.example/unchanged"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: unchanged}}); err != nil {
		t.Fatal(err)
	}
	if err := documents.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for _, targetURL := range []string{missing, unchanged} {
			if err := documents.storeInboundAnchorRefresh(
				tx,
				targetURL,
				inboundAnchorRefreshFor(map[string]struct{}{"https://source.example/": {}}),
			); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if reindexed := drainInboundAnchorRefreshes(t, receiver); len(reindexed) != 0 {
		t.Fatalf("refresh without inlink change reindexed %#v", reindexed)
	}
	for _, targetURL := range []string{missing, unchanged} {
		if _, pending := pendingInboundAnchorRefresh(t, receiver, targetURL); pending {
			t.Fatalf("refresh mark for %s survived", targetURL)
		}
	}
}

func TestInboundAnchorRefreshMergeCapsReplacedSources(t *testing.T) {
	sources := make(map[string]struct{}, maximumInboundAnchorRefreshSources)
	for sequence := range maximumInboundAnchorRefreshSources {
		sources[fmt.Sprintf("https://source.example/%03d", sequence)] = struct{}{}
	}
	atCapacity := *inboundAnchorRefreshFor(sources)
	if atCapacity.AllSourcesReplaced ||
		len(atCapacity.ReplacedSources) != maximumInboundAnchorRefreshSources {
		t.Fatalf(
			"refresh at capacity = %d/%t",
			len(atCapacity.ReplacedSources),
			atCapacity.AllSourcesReplaced,
		)
	}
	overflow := atCapacity.merged(inboundAnchorRefresh{
		ReplacedSources: []string{"https://source.example/overflow"},
	})
	if !overflow.AllSourcesReplaced || len(overflow.ReplacedSources) != 0 {
		t.Fatalf("refresh over capacity = %#v", overflow)
	}
	if merged := overflow.merged(atCapacity); !merged.AllSourcesReplaced {
		t.Fatalf("all-sources refresh narrowed after merge: %#v", merged)
	}
	submitted := AnchorText{URL: "https://submitted.example/", Text: "submitted"}
	materialized := []AnchorText{{URL: "https://source.example/000", Text: "current"}}
	if inlinks := overflow.refreshedInlinks(
		[]AnchorText{submitted},
		materialized,
	); !slices.Equal(inlinks, materialized) {
		t.Fatalf("all-sources refresh inlinks = %#v", inlinks)
	}
	if inlinks := atCapacity.refreshedInlinks(
		[]AnchorText{submitted},
		materialized,
	); !slices.Equal(inlinks, []AnchorText{materialized[0], submitted}) {
		t.Fatalf("bounded refresh inlinks = %#v", inlinks)
	}
}

func TestInboundAnchorRefreshRefusesNonPositiveLimit(t *testing.T) {
	_, receiver := openDocuments(t)
	if _, err := receiver.(InboundAnchorDocumentRefresher).RefreshInboundAnchorDocuments(
		t.Context(),
		0,
		func([]Document) error { return nil },
	); err == nil {
		t.Fatal("refresh accepted a zero page limit")
	}
}

func TestDocumentRevisionAppliesPendingInboundAnchorRefresh(t *testing.T) {
	directory, receiver := openDocuments(t)
	revisions := directory.(DocumentRevisionDirectory)
	pending := "https://target.example/revision-pending"
	settled := "https://target.example/revision-settled"
	kept := AnchorText{URL: "https://kept.example/", Text: "kept"}
	if _, err := receiver.Receive(t.Context(), []Document{
		{NormalizedURL: pending},
		{NormalizedURL: settled, Inlinks: []AnchorText{kept}},
	}); err != nil {
		t.Fatal(err)
	}
	source := "https://source.example/revision"
	replaceOutboundAnchorText(t, receiver, source, pending, "pending")

	revision, found, err := revisions.DocumentRevision(t.Context(), pending)
	if err != nil || !found || !documentHasInboundAnchorSource(revision, source) {
		t.Fatalf("pending revision = %#v/%t/%v", revision.Inlinks, found, err)
	}
	revision, found, err = revisions.DocumentRevision(t.Context(), settled)
	if err != nil || !found || !slices.Equal(revision.Inlinks, []AnchorText{kept}) {
		t.Fatalf("settled revision = %#v/%t/%v", revision.Inlinks, found, err)
	}
}

func TestOutboundAnchorReplacementOutsideCapacityKeepsPendingRefresh(t *testing.T) {
	_, receiver, engine := openOutboundAnchorObservedDocuments(t)
	hub := "https://hub.example/pending"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: hub}}); err != nil {
		t.Fatal(err)
	}
	replacer := receiver.(OutboundAnchorDocumentReplacer)
	fillInboundAnchorCapacityWithoutRefresh(t, replacer, hub)
	before, pending := pendingInboundAnchorRefresh(t, receiver, hub)
	if !pending {
		t.Fatal("filled hub has no pending refresh")
	}
	engine.clearObservations()

	replaceOutboundAnchorText(t, receiver, "https://z.example/late", hub, "hub")
	for _, observation := range engine.observations() {
		if observation.puts[inboundAnchorRefreshBucket] != 0 {
			t.Fatalf("source outside capacity rewrote the pending refresh: %#v", observation)
		}
	}
	replaceOutboundAnchorText(t, receiver, "https://0.example/early", hub, "hub")
	after, _ := pendingInboundAnchorRefresh(t, receiver, hub)
	if after.AllSourcesReplaced == before.AllSourcesReplaced &&
		len(after.ReplacedSources) == len(before.ReplacedSources) {
		t.Fatalf("source inside capacity left the pending refresh unchanged: %#v", after)
	}
}

func TestOutboundAnchorReplacementRefusesCorruptPendingRefresh(t *testing.T) {
	_, documents, engine := openDocumentStorageFaultVault(t)
	targetURL := "https://target.example/corrupt-refresh"
	if _, err := documents.Receive(
		t.Context(),
		[]Document{{NormalizedURL: targetURL}},
	); err != nil {
		t.Fatal(err)
	}
	engine.putRaw(inboundAnchorRefreshBucket, vault.Key(targetURL), []byte("{"))
	if _, err := documents.ReplaceOutboundAnchorDocuments(
		t.Context(),
		outboundAnchorErrorSet("corrupt-refresh", targetURL),
	); err == nil {
		t.Fatal("corrupt pending refresh was accepted")
	}
	if _, _, err := documents.DocumentRevision(t.Context(), targetURL); err == nil {
		t.Fatal("revision accepted a corrupt pending refresh")
	}
}

func TestInboundAnchorRefreshSurvivesStorageRestart(t *testing.T) {
	engine := newOutboundAnchorObservingEngine()
	_, receiver := openDocumentsOnEngine(t, engine)
	target := "https://target.example/restart"
	source := "https://source.example/restart"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: target}}); err != nil {
		t.Fatal(err)
	}
	replaceOutboundAnchorText(t, receiver, source, target, "restart-safe")
	if _, err := receiver.(InboundAnchorDocumentRefresher).RefreshInboundAnchorDocuments(
		t.Context(),
		outboundAnchorTargetPageSize,
		func([]Document) error { return errors.New("index interrupted") },
	); err == nil {
		t.Fatal("interrupted refresh reported success")
	}

	directory, reopened := openDocumentsOnEngine(t, engine)
	if reindexed := drainInboundAnchorRefreshes(t, reopened); len(reindexed) != 1 {
		t.Fatalf("refresh after restart reindexed %d documents, want 1", len(reindexed))
	}
	stored, found, err := directory.Document(t.Context(), target)
	if err != nil || !found || !documentHasInboundAnchorSource(stored, source) {
		t.Fatalf("target after restart = %#v/%t/%v", stored.Inlinks, found, err)
	}
}

func openDocumentsOnEngine(
	t *testing.T,
	engine vault.Engine,
) (DocumentDirectory, DocumentReceiver) {
	t.Helper()
	vaulted, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	directory, receiver, err := Open(vaulted)
	if err != nil {
		t.Fatal(err)
	}

	return directory, receiver
}
