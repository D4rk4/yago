package documentstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOutboundAnchorDocumentReplacementBoundsMaximumInputTransactions(t *testing.T) {
	_, receiver, engine := openOutboundAnchorObservedDocuments(t)
	sets := make([]OutboundAnchorSet, 0, MaximumOutboundAnchorSourcesPerReplacement)
	wantTargets := make(
		[]string,
		0,
		MaximumOutboundAnchorSourcesPerReplacement*maximumOutboundAnchors,
	)
	for source := range MaximumOutboundAnchorSourcesPerReplacement {
		anchors := make([]OutboundAnchor, 0, maximumOutboundAnchors)
		for edge := range maximumOutboundAnchors {
			targetURL := fmt.Sprintf(
				"https://target.example/%02d/%04d",
				source,
				edge,
			)
			anchors = append(anchors, OutboundAnchor{
				TargetURL: targetURL,
				Text:      "bounded",
			})
			wantTargets = append(wantTargets, targetURL)
		}
		sets = append(sets, OutboundAnchorSet{
			SourceURL: fmt.Sprintf("https://source.example/%02d", source),
			Anchors:   anchors,
		})
	}
	if _, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		sets,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	mutations := outboundAnchorMutationObservations(engine.observations())
	if len(mutations) != len(wantTargets)/outboundAnchorTargetPageSize {
		t.Fatalf("mutation transactions = %d", len(mutations))
	}
	gotTargets := make([]string, 0, len(wantTargets))
	for index, mutation := range mutations {
		if mutation.err != nil ||
			mutation.rows > outboundAnchorMutationMaximumRows ||
			len(mutation.targets) > outboundAnchorTargetPageSize ||
			mutation.encodedBytes > outboundAnchorMutationMaximumEncodedBytes {
			t.Fatalf("mutation transaction %d = %#v", index, mutation)
		}
		gotTargets = append(gotTargets, mutation.targets...)
	}
	if !slices.Equal(gotTargets, wantTargets) {
		t.Fatal("maximum replacement target transactions are not deterministic")
	}
}

func TestOutboundAnchorDocumentReplacementBoundsTransactionEncodedBytes(t *testing.T) {
	_, receiver, engine := openOutboundAnchorObservedDocuments(t)
	targets := outboundAnchorReplacementTargets(outboundAnchorTargetPageSize)
	documents := make([]Document, 0, len(targets))
	for _, targetURL := range targets {
		documents = append(documents, Document{
			NormalizedURL: targetURL,
			ExtractedText: strings.Repeat("x", 700<<10),
		})
	}
	if _, err := receiver.Receive(t.Context(), documents); err != nil {
		t.Fatal(err)
	}
	engine.clearObservations()
	if _, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: "https://source.example/byte-budget",
			Anchors:   outboundAnchorReplacementEdges(targets),
		}},
		func(documents []Document) error {
			if len(documents) != outboundAnchorTargetPageSize {
				t.Fatalf("projection page = %d documents", len(documents))
			}

			return nil
		},
	); err != nil {
		t.Fatal(err)
	}
	mutations := outboundAnchorMutationObservations(engine.observations())
	if len(mutations) < 2 {
		t.Fatalf("byte-bound mutation transactions = %d, want at least 2", len(mutations))
	}
	for index, mutation := range mutations {
		if mutation.err != nil ||
			mutation.rows > outboundAnchorMutationMaximumRows ||
			len(mutation.targets) > outboundAnchorTargetPageSize ||
			mutation.encodedBytes > outboundAnchorMutationMaximumEncodedBytes {
			t.Fatalf("byte-bound transaction %d = %#v", index, mutation)
		}
	}
}

func TestOutboundAnchorDocumentReplacementAggregatesSourcesPerTarget(t *testing.T) {
	directory, receiver, engine := openOutboundAnchorObservedDocuments(t)
	targetURL := "https://target.example/aggregate"
	if _, err := receiver.Receive(
		t.Context(),
		[]Document{{NormalizedURL: targetURL}},
	); err != nil {
		t.Fatal(err)
	}
	engine.clearObservations()
	sets := make([]OutboundAnchorSet, 0, MaximumOutboundAnchorSourcesPerReplacement)
	for source := range MaximumOutboundAnchorSourcesPerReplacement {
		sourceURL := fmt.Sprintf("https://source.example/aggregate/%02d", source)
		sets = append(sets, OutboundAnchorSet{
			SourceURL: sourceURL,
			Anchors: []OutboundAnchor{{
				TargetURL: targetURL,
				Text:      sourceURL,
			}},
		})
	}
	if _, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		sets,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	mutations := outboundAnchorMutationObservations(engine.observations())
	if len(mutations) != 1 || !slices.Equal(mutations[0].targets, []string{targetURL}) ||
		mutations[0].puts[inboundAnchorBucket] != 1 ||
		mutations[0].puts[orderedDocumentBucketName] != 1 {
		t.Fatalf("aggregated target mutations = %#v", mutations)
	}
	if gets := engine.getTotal(inboundAnchorBucket, targetURL); gets != 1 {
		t.Fatalf("target inbound reads = %d, want 1", gets)
	}
	document, found, err := directory.Document(t.Context(), targetURL)
	if err != nil || !found || len(document.Inlinks) != len(sets) {
		t.Fatalf("aggregated target document = %#v/%t/%v", document, found, err)
	}
}

func TestOutboundAnchorDocumentReplacementStorageFailureReplaysFromOldPublication(
	t *testing.T,
) {
	directory, receiver, engine := openOutboundAnchorObservedDocuments(t)
	documents := receiver.(documentVault)
	targets := outboundAnchorReplacementTargets(40)
	stored := make([]Document, 0, len(targets))
	for _, targetURL := range targets {
		stored = append(stored, Document{NormalizedURL: targetURL})
	}
	if _, err := receiver.Receive(t.Context(), stored); err != nil {
		t.Fatal(err)
	}
	sourceURL := "https://source.example/storage-replay"
	replacer := receiver.(OutboundAnchorDocumentReplacer)
	oldTargets := targets[:20]
	newTargets := targets[10:]
	if _, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: sourceURL,
			Anchors:   outboundAnchorReplacementEdges(oldTargets),
		}},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	wantFailure := errors.New("target storage failed")
	engine.failOnce(targets[24], wantFailure)
	_, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: sourceURL,
			Anchors:   outboundAnchorReplacementEdges(newTargets),
		}},
		nil,
	)
	if !errors.Is(err, wantFailure) {
		t.Fatalf("storage failure = %v", err)
	}
	publication := readOutboundAnchorReplacementPublication(t, documents, sourceURL)
	if !slices.Equal(publication.Targets, oldTargets) {
		t.Fatalf("publication after storage failure = %#v", publication.Targets)
	}
	assertPromptStoredDocument(t, directory, targets[24])
	engine.clearFailure()
	if _, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: sourceURL,
			Anchors:   outboundAnchorReplacementEdges(newTargets),
		}},
		nil,
	); err != nil {
		t.Fatalf("storage replay: %v", err)
	}
	publication = readOutboundAnchorReplacementPublication(t, documents, sourceURL)
	if !slices.Equal(publication.Targets, newTargets) {
		t.Fatalf("publication after storage replay = %#v", publication.Targets)
	}
	removed, found, err := directory.Document(t.Context(), targets[0])
	if err != nil || !found || documentHasInboundAnchorSource(removed, sourceURL) {
		t.Fatalf("removed storage replay target = %#v/%t/%v", removed, found, err)
	}
	added, found, err := directory.Document(t.Context(), targets[39])
	if err != nil || !found || !documentHasInboundAnchorSource(added, sourceURL) {
		t.Fatalf("added storage replay target = %#v/%t/%v", added, found, err)
	}
}

func TestOutboundAnchorDocumentReplacementSerializesOverlappingTargetWriters(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
	targetURL := "https://target.example/overlap"
	if _, err := receiver.Receive(
		t.Context(),
		[]Document{{NormalizedURL: targetURL}},
	); err != nil {
		t.Fatal(err)
	}
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
			t.Context(),
			outboundAnchorOverlapSet("first", targetURL),
			func([]Document) error {
				close(firstEntered)
				<-releaseFirst

				return nil
			},
		)
		firstDone <- err
	}()
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first overlapping writer did not reach projection")
	}
	secondDone := make(chan error, 1)
	go func() {
		_, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
			t.Context(),
			outboundAnchorOverlapSet("second", targetURL),
			nil,
		)
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("overlapping writer crossed current target page: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseFirst)
	for identity, result := range map[string]<-chan error{
		"first":  firstDone,
		"second": secondDone,
	} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("%s overlapping writer: %v", identity, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s overlapping writer remained blocked", identity)
		}
	}
	document, found, err := directory.Document(t.Context(), targetURL)
	if err != nil || !found || len(document.Inlinks) != 2 {
		t.Fatalf("overlapping target = %#v/%t/%v", document, found, err)
	}
}

func TestOutboundAnchorDocumentReplacementRetainsSourceLineageUntilPublication(t *testing.T) {
	_, receiver, _ := openPagedDocuments(t)
	firstTarget := "https://target.example/source-lineage/first"
	secondTarget := "https://target.example/source-lineage/second"
	if _, err := receiver.Receive(t.Context(), []Document{
		{NormalizedURL: firstTarget},
		{NormalizedURL: secondTarget},
	}); err != nil {
		t.Fatal(err)
	}
	sourceURL := "https://source.example/serialized"
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
			t.Context(),
			[]OutboundAnchorSet{{
				SourceURL: sourceURL,
				Anchors:   outboundAnchorReplacementEdges([]string{firstTarget}),
			}},
			func([]Document) error {
				close(firstEntered)
				<-releaseFirst

				return nil
			},
		)
		firstDone <- err
	}()
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first source writer did not reach projection")
	}
	secondDone := make(chan error, 1)
	go func() {
		_, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
			t.Context(),
			[]OutboundAnchorSet{{
				SourceURL: sourceURL,
				Anchors:   outboundAnchorReplacementEdges([]string{secondTarget}),
			}},
			nil,
		)
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("same-source writer crossed unpublished replacement: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseFirst)
	for _, result := range []<-chan error{firstDone, secondDone} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("same-source writer remained blocked")
		}
	}
	publication := readOutboundAnchorReplacementPublication(
		t,
		receiver.(documentVault),
		sourceURL,
	)
	if !slices.Equal(publication.Targets, []string{secondTarget}) {
		t.Fatalf("same-source publication = %#v", publication.Targets)
	}
}

func openOutboundAnchorObservedDocuments(
	t *testing.T,
) (DocumentDirectory, DocumentReceiver, *outboundAnchorObservingEngine) {
	t.Helper()
	engine := newOutboundAnchorObservingEngine()
	vaulted, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = vaulted.Close() })
	directory, receiver, err := Open(vaulted)
	if err != nil {
		t.Fatal(err)
	}
	engine.clearObservations()

	return directory, receiver, engine
}

func outboundAnchorOverlapSet(identity string, targetURL string) []OutboundAnchorSet {
	return []OutboundAnchorSet{{
		SourceURL: "https://source.example/overlap/" + identity,
		Anchors: []OutboundAnchor{{
			TargetURL: targetURL,
			Text:      identity,
		}},
	}}
}

func TestOutboundAnchorMutationBatchesHonorRowsBytesAndOrder(t *testing.T) {
	mutations := []outboundAnchorTargetMutation{
		{targetURL: "ignored"},
		{targetURL: "a", storeAnchors: true, encodedBytes: 4},
		{targetURL: "b", storeAnchors: true, encodedBytes: 4},
		{targetURL: "c", storeAnchors: true, encodedBytes: 4},
	}
	batches, err := outboundAnchorTargetMutationBatches(mutations, 2, 6)
	if err != nil || len(batches) != 3 ||
		batches[0][0].targetURL != "a" ||
		batches[1][0].targetURL != "b" ||
		batches[2][0].targetURL != "c" {
		t.Fatalf("mutation batches = %#v/%v", batches, err)
	}
	for _, limits := range [][2]int{{0, 1}, {1, 0}} {
		if _, err := outboundAnchorTargetMutationBatches(
			mutations,
			limits[0],
			limits[1],
		); err == nil {
			t.Fatalf("invalid mutation limits = %#v", limits)
		}
	}
	mutations[1].encodedBytes = -1
	if _, err := outboundAnchorTargetMutationBatches(mutations, 1, 1); err == nil {
		t.Fatal("negative mutation size was accepted")
	}
	mutations[1] = outboundAnchorTargetMutation{
		storeAnchors:  true,
		storeDocument: true,
	}
	if _, err := outboundAnchorTargetMutationBatches(mutations, 1, 1); err == nil {
		t.Fatal("oversized mutation row set was accepted")
	}
}

func TestOutboundAnchorPublicationGroupsUseEncodedByteCeiling(t *testing.T) {
	finalizations := []OutboundAnchorFinalization{
		{
			sourceURL: "b",
			desired: outboundAnchorPublication{
				Targets: []string{"https://target.example/b"},
			},
		},
		{
			sourceURL: "a",
			desired: outboundAnchorPublication{
				Targets: []string{"https://target.example/a"},
			},
		},
	}
	maximum := 0
	for _, finalization := range finalizations {
		maximum += outboundAnchorFinalizationEncodedByteCeiling(finalization)
	}
	groups, err := outboundAnchorPublicationGroups(finalizations, maximum)
	if err != nil || len(groups) != 1 {
		t.Fatalf("full publication group = %#v/%v", groups, err)
	}
	groups, err = outboundAnchorPublicationGroups(finalizations, maximum-1)
	if err != nil || len(groups) != 2 {
		t.Fatalf("byte-split publication groups = %#v/%v", groups, err)
	}
}

func TestOutboundAnchorObservingEnginePropagatesCanceledUpdate(t *testing.T) {
	engine := newOutboundAnchorObservingEngine()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := engine.Update(canceled, func(vault.EngineTxn) error {
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled observed update = %v", err)
	}
}
