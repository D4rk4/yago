package documentstore

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOutboundAnchorPublicationRecoversTargetReplacementAndRemoval(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/page"
	firstTarget := "https://target.example/first"
	secondTarget := "https://target.example/second"
	if _, err := receiver.Receive(t.Context(), []Document{
		{NormalizedURL: firstTarget},
		{NormalizedURL: secondTarget},
	}); err != nil {
		t.Fatal(err)
	}
	firstSet := OutboundAnchorSet{
		SourceURL: source,
		Anchors:   []OutboundAnchor{{TargetURL: firstTarget, Text: "first"}},
	}
	replaceAndFinalizeOutboundAnchors(t, receiver, []OutboundAnchorSet{firstSet})
	secondSet := OutboundAnchorSet{
		SourceURL: source,
		Anchors:   []OutboundAnchor{{TargetURL: secondTarget, Text: "second"}},
	}
	pending, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{secondSet})
	if err != nil {
		t.Fatalf("pending replacement = %#v, %v", pending, err)
	}
	pendingDocuments := collectOutboundAnchorDocuments(t, documents, pending.Finalizations)
	if len(pendingDocuments) != 2 || len(pending.Finalizations) != 1 {
		t.Fatalf("pending replacement documents = %#v, update = %#v", pendingDocuments, pending)
	}
	assertOutboundAnchorPublicationTargets(t, documents, source, firstTarget)
	documents.ReleaseOutboundAnchors(pending.Finalizations)
	replayed, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{secondSet})
	if err != nil {
		t.Fatalf("replayed replacement = %#v, %v", replayed, err)
	}
	replayedDocuments := collectOutboundAnchorDocuments(t, documents, replayed.Finalizations)
	if len(replayedDocuments) != 2 || len(replayed.Finalizations) != 1 {
		t.Fatalf("replayed replacement documents = %#v, update = %#v", replayedDocuments, replayed)
	}
	if err := documents.FinalizeOutboundAnchors(t.Context(), replayed.Finalizations); err != nil {
		t.Fatal(err)
	}
	assertOutboundAnchorPublicationTargets(t, documents, source, secondTarget)

	pending, err = documents.ReplaceOutboundAnchors(
		t.Context(),
		[]OutboundAnchorSet{{SourceURL: source}},
	)
	if err != nil {
		t.Fatalf("pending removal = %#v, %v", pending, err)
	}
	pendingDocuments = collectOutboundAnchorDocuments(t, documents, pending.Finalizations)
	if len(pendingDocuments) != 1 || len(pending.Finalizations) != 1 {
		t.Fatalf("pending removal documents = %#v, update = %#v", pendingDocuments, pending)
	}
	assertOutboundAnchorPublicationTargets(t, documents, source, secondTarget)
	documents.ReleaseOutboundAnchors(pending.Finalizations)
	replayed, err = documents.ReplaceOutboundAnchors(
		t.Context(),
		[]OutboundAnchorSet{{SourceURL: source}},
	)
	if err != nil {
		t.Fatalf("replayed removal = %#v, %v", replayed, err)
	}
	replayedDocuments = collectOutboundAnchorDocuments(t, documents, replayed.Finalizations)
	if len(replayedDocuments) != 1 || len(replayed.Finalizations) != 1 {
		t.Fatalf("replayed removal documents = %#v, update = %#v", replayedDocuments, replayed)
	}
	if err := documents.FinalizeOutboundAnchors(t.Context(), replayed.Finalizations); err != nil {
		t.Fatal(err)
	}
	assertOutboundAnchorPublicationTargets(t, documents, source)
	if engine.buckets[outboundAnchorPublicationBucket][source] == nil {
		t.Fatal("empty publication did not shadow prior target state")
	}
}

func TestOutboundAnchorPublicationRecoversSameTargetTextChange(t *testing.T) {
	directory, receiver, _ := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/page"
	target := "https://target.example/page"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: target}}); err != nil {
		t.Fatal(err)
	}
	replaceAndFinalizeOutboundAnchors(t, receiver, []OutboundAnchorSet{{
		SourceURL: source,
		Anchors:   []OutboundAnchor{{TargetURL: target, Text: "old"}},
	}})
	changed := OutboundAnchorSet{
		SourceURL: source,
		Anchors:   []OutboundAnchor{{TargetURL: target, Text: "new"}},
	}
	first, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{changed})
	if err != nil {
		t.Fatalf("changed phase = %#v, %v", first, err)
	}
	firstDocuments := collectOutboundAnchorDocuments(t, documents, first.Finalizations)
	if len(firstDocuments) != 1 || len(first.Finalizations) != 1 {
		t.Fatalf("changed phase documents = %#v, update = %#v", firstDocuments, first)
	}
	documents.ReleaseOutboundAnchors(first.Finalizations)
	second, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{changed})
	if err != nil {
		t.Fatalf("changed replay = %#v, %v", second, err)
	}
	secondDocuments := collectOutboundAnchorDocuments(t, documents, second.Finalizations)
	if len(secondDocuments) != 1 || len(second.Finalizations) != 1 {
		t.Fatalf("changed replay documents = %#v, update = %#v", secondDocuments, second)
	}
	if len(secondDocuments[0].Inlinks) != 1 || secondDocuments[0].Inlinks[0].Text != "new" {
		t.Fatalf("changed projection = %#v", secondDocuments[0].Inlinks)
	}
	if err := documents.FinalizeOutboundAnchors(t.Context(), second.Finalizations); err != nil {
		t.Fatal(err)
	}
	stored, found, err := directory.Document(t.Context(), target)
	if err != nil || !found || len(stored.Inlinks) != 1 || stored.Inlinks[0].Text != "new" {
		t.Fatalf("changed target = %#v/%t/%v", stored, found, err)
	}
	unchanged, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{changed})
	if err != nil {
		t.Fatalf("finalized replay = %#v, %v", unchanged, err)
	}
	unchangedDocuments := collectOutboundAnchorDocuments(t, documents, unchanged.Finalizations)
	if len(unchangedDocuments) != 0 || len(unchanged.Finalizations) != 0 {
		t.Fatalf("finalized replay documents = %#v, update = %#v", unchangedDocuments, unchanged)
	}
}

func TestOutboundAnchorPublicationRepairsMissingInboundProjection(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/partial-inbound"
	target := "https://target.example/partial-inbound"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: target}}); err != nil {
		t.Fatal(err)
	}
	set := OutboundAnchorSet{
		SourceURL: source,
		Anchors:   []OutboundAnchor{{TargetURL: target, Text: "durable"}},
	}
	pending, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{set})
	if err != nil {
		t.Fatal(err)
	}
	if projected := collectOutboundAnchorDocuments(
		t,
		documents,
		pending.Finalizations,
	); len(projected) != 1 {
		t.Fatalf("pending projection = %#v", projected)
	}
	documents.ReleaseOutboundAnchors(pending.Finalizations)
	delete(engine.buckets[inboundAnchorBucket], target)
	replayed, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{set})
	if err != nil {
		t.Fatal(err)
	}
	projected := collectOutboundAnchorDocuments(t, documents, replayed.Finalizations)
	if len(projected) != 1 || len(projected[0].Inlinks) != 1 {
		t.Fatalf("repaired projection = %#v", projected)
	}
	if engine.buckets[inboundAnchorBucket][target] == nil {
		t.Fatal("missing inbound projection was not repaired")
	}
	if err := documents.FinalizeOutboundAnchors(
		t.Context(),
		replayed.Finalizations,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Receive(t.Context(), []Document{
		{NormalizedURL: target, Title: "recrawled"},
	}); err != nil {
		t.Fatal(err)
	}
	stored, found, err := directory.Document(t.Context(), target)
	if err != nil || !found || len(stored.Inlinks) != 1 ||
		stored.Inlinks[0].Text != "durable" {
		t.Fatalf("recrawled target = %#v/%t/%v", stored, found, err)
	}
}

func TestOutboundAnchorFinalizationRetriesFailedAndAmbiguousCommits(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/page"
	target := "https://target.example/page"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: target}}); err != nil {
		t.Fatal(err)
	}
	set := OutboundAnchorSet{
		SourceURL: source,
		Anchors:   []OutboundAnchor{{TargetURL: target, Text: "stable"}},
	}
	update, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{set})
	if err != nil {
		t.Fatal(err)
	}
	updateDocuments := collectOutboundAnchorDocuments(t, documents, update.Finalizations)
	if len(updateDocuments) != 1 {
		t.Fatalf("initial projection documents = %#v", updateDocuments)
	}
	engine.putErrors[outboundAnchorPublicationBucket] = errors.New("publication failed")
	if err := documents.FinalizeOutboundAnchors(
		t.Context(),
		update.Finalizations,
	); err == nil {
		t.Fatal("failed finalization was accepted")
	}
	delete(engine.putErrors, outboundAnchorPublicationBucket)
	replayed, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{set})
	if err != nil {
		t.Fatalf("failed finalization replay = %#v, %v", replayed, err)
	}
	replayedDocuments := collectOutboundAnchorDocuments(t, documents, replayed.Finalizations)
	if len(replayedDocuments) != 1 {
		t.Fatalf("failed finalization replay documents = %#v", replayedDocuments)
	}
	engine.putCommitErrors[outboundAnchorPublicationBucket] = errors.New("commit outcome unknown")
	if err := documents.FinalizeOutboundAnchors(
		t.Context(),
		replayed.Finalizations,
	); err == nil {
		t.Fatal("ambiguous finalization outcome was not returned")
	}
	delete(engine.putCommitErrors, outboundAnchorPublicationBucket)
	settled, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{set})
	if err != nil {
		t.Fatalf("ambiguous finalization replay = %#v, %v", settled, err)
	}
	settledDocuments := collectOutboundAnchorDocuments(t, documents, settled.Finalizations)
	if len(settledDocuments) != 0 || len(settled.Finalizations) != 0 {
		t.Fatalf(
			"ambiguous finalization replay documents = %#v, update = %#v",
			settledDocuments,
			settled,
		)
	}
	assertOutboundAnchorPublicationTargets(t, documents, source, target)
}

func TestOutboundAnchorFinalizationRejectsStaleAndInvalidTokens(t *testing.T) {
	_, receiver, _ := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/page"
	first := desiredOutboundAnchorPublication(
		map[string][]AnchorText{"https://target.example/first": {{URL: source}}},
		[]string{"https://target.example/first"},
	)
	second := desiredOutboundAnchorPublication(
		map[string][]AnchorText{"https://target.example/second": {{URL: source}}},
		[]string{"https://target.example/second"},
	)
	if err := documents.FinalizeOutboundAnchors(t.Context(), []OutboundAnchorFinalization{{
		sourceURL: source,
		desired:   first,
		lease:     newOutboundAnchorLease(func() {}, first.Targets),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := documents.FinalizeOutboundAnchors(t.Context(), []OutboundAnchorFinalization{{
		sourceURL: source,
		desired:   second,
		lease:     newOutboundAnchorLease(func() {}, second.Targets),
	}}); err == nil {
		t.Fatal("stale finalization was accepted")
	}
	if err := documents.FinalizeOutboundAnchors(
		t.Context(),
		[]OutboundAnchorFinalization{{}},
	); err == nil {
		t.Fatal("blank finalization source was accepted")
	}
	duplicate := OutboundAnchorFinalization{
		sourceURL: source,
		expected:  first,
		desired:   first,
		lease:     newOutboundAnchorLease(func() {}, first.Targets),
	}
	if err := documents.FinalizeOutboundAnchors(
		t.Context(),
		[]OutboundAnchorFinalization{duplicate, duplicate},
	); err == nil {
		t.Fatal("duplicate finalization source was accepted")
	}
}

func TestOutboundAnchorPublicationBoundsIdentitiesAndVirginEmptyWrites(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	validSource := "https://source.example/page"
	oversized := strings.Repeat("x", yagomodel.MaximumURLIdentityBytes+1)
	update, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{
		{SourceURL: oversized, Anchors: []OutboundAnchor{{TargetURL: "https://target.example/"}}},
		{SourceURL: validSource, Anchors: []OutboundAnchor{{TargetURL: oversized}}},
	})
	if err != nil {
		t.Fatalf("bounded update = %#v, %v", update, err)
	}
	projection := collectOutboundAnchorDocuments(t, documents, update.Finalizations)
	if len(projection) != 0 || len(update.Finalizations) != 0 {
		t.Fatalf("bounded projection = %#v, update = %#v", projection, update)
	}
	if len(engine.buckets[inboundAnchorBucket]) != 0 ||
		len(engine.buckets[outboundAnchorPublicationBucket]) != 0 {
		t.Fatalf(
			"bounded anchor rows/publications = %d/%d",
			len(engine.buckets[inboundAnchorBucket]),
			len(engine.buckets[outboundAnchorPublicationBucket]),
		)
	}
	if err := documents.FinalizeOutboundAnchors(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestOutboundAnchorPublicationShadowsLegacyTargetsWithEmptyState(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/legacy"
	target := "https://target.example/legacy"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: target}}); err != nil {
		t.Fatal(err)
	}
	anchors, err := (anchorJSONCodec[[]AnchorText]{}).Encode([]AnchorText{{
		URL:  source,
		Text: "legacy",
	}})
	if err != nil {
		t.Fatal(err)
	}
	targets, err := (anchorJSONCodec[[]string]{}).Encode([]string{target})
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[inboundAnchorBucket][target] = anchors
	engine.buckets[outboundTargetBucket][source] = targets
	update, err := documents.ReplaceOutboundAnchors(
		t.Context(),
		[]OutboundAnchorSet{{SourceURL: source}},
	)
	if err != nil {
		t.Fatalf("legacy removal = %#v, %v", update, err)
	}
	projection := collectOutboundAnchorDocuments(t, documents, update.Finalizations)
	if len(projection) != 1 || len(update.Finalizations) != 1 {
		t.Fatalf("legacy removal projection = %#v, update = %#v", projection, update)
	}
	if err := documents.FinalizeOutboundAnchors(t.Context(), update.Finalizations); err != nil {
		t.Fatal(err)
	}
	if engine.buckets[outboundTargetBucket][source] == nil {
		t.Fatal("legacy target marker was unexpectedly mutated")
	}
	assertOutboundAnchorPublicationTargets(t, documents, source)
}

func TestOutboundAnchorProjectionVisitsBoundedBatches(t *testing.T) {
	_, receiver := openDocuments(t)
	anchors := anchorReceiver(t, receiver)
	documents := make([]Document, 33)
	outbound := make([]OutboundAnchor, len(documents))
	for index := range documents {
		targetURL := fmt.Sprintf("https://target.example/%02d", index)
		documents[index].NormalizedURL = targetURL
		outbound[index] = OutboundAnchor{TargetURL: targetURL, Text: "anchor"}
	}
	if _, err := receiver.Receive(t.Context(), documents); err != nil {
		t.Fatalf("receive targets: %v", err)
	}
	update, err := anchors.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{{
		SourceURL: "https://source.example/page",
		Anchors:   outbound,
	}})
	if err != nil {
		t.Fatalf("replace outbound anchors: %v", err)
	}
	batchSizes := make([]int, 0)
	if err := anchors.VisitOutboundAnchorDocuments(
		t.Context(),
		update.Finalizations,
		func(batch []Document) error {
			batchSizes = append(batchSizes, len(batch))

			return nil
		},
	); err != nil {
		t.Fatalf("visit outbound anchor documents: %v", err)
	}
	if !slices.Equal(batchSizes, []int{16, 16, 1}) {
		t.Fatalf("projection batch sizes = %#v", batchSizes)
	}
	if err := anchors.FinalizeOutboundAnchors(t.Context(), update.Finalizations); err != nil {
		t.Fatalf("finalize outbound anchors: %v", err)
	}
}

func assertOutboundAnchorPublicationTargets(
	t *testing.T,
	documents documentVault,
	sourceURL string,
	want ...string,
) {
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
	if !slices.Equal(publication.Targets, want) {
		t.Fatalf("publication targets = %#v, want %#v", publication.Targets, want)
	}
}
