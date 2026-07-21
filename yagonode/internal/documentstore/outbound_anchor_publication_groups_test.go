package documentstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestOutboundAnchorPublicationGroupsAdaptMaximumLongURLReplacement(t *testing.T) {
	_, receiver, engine := openOutboundAnchorObservedDocuments(t)
	sets := outboundAnchorMaximumLongPublicationSets()
	canonical, err := canonicalOutboundAnchorSets(sets)
	if err != nil {
		t.Fatal(err)
	}
	finalizations := outboundAnchorPublicationGroupFinalizations(canonical)
	groups := requireBoundedOutboundAnchorPublicationGroups(t, finalizations, canonical)
	if _, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		sets,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	assertOutboundAnchorReplacementTransactionBounds(t, engine.observations(), len(groups))
	assertOutboundAnchorMaximumPublications(t, receiver.(documentVault), canonical)
}

func TestOutboundAnchorPublicationGroupsValidateBudgetAndPreserveOrder(t *testing.T) {
	sets := []OutboundAnchorSet{
		outboundAnchorPublicationGroupSet("00", "00"),
		outboundAnchorPublicationGroupSet("01", "01"),
	}
	finalizations := outboundAnchorPublicationGroupFinalizations(sets)
	maximumEncodedBytes := outboundAnchorFinalizationEncodedByteCeiling(finalizations[0])
	groups, err := outboundAnchorPublicationGroups(finalizations, maximumEncodedBytes)
	if err != nil || len(groups) != 2 ||
		groups[0][0].sourceURL != sets[0].SourceURL ||
		groups[1][0].sourceURL != sets[1].SourceURL {
		t.Fatalf("publication groups = %#v/%v", groups, err)
	}
	if groups, err := outboundAnchorPublicationGroups(nil, 1); err != nil || len(groups) != 0 {
		t.Fatalf("empty publication groups = %#v/%v", groups, err)
	}
	if _, err := outboundAnchorPublicationGroups(finalizations, 0); err == nil {
		t.Fatal("zero publication budget was accepted")
	}
	if _, err := outboundAnchorPublicationGroups(
		finalizations[:1],
		maximumEncodedBytes-1,
	); err == nil {
		t.Fatal("oversized source publication was accepted")
	}
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
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
		sets[:1],
		nil,
		0,
	); err == nil {
		t.Fatal("replacement accepted zero publication budget")
	}
}

func TestOutboundAnchorPublicationGroupFailureRetainsPendingPublicationsAndReplays(
	t *testing.T,
) {
	directory, receiver, engine := openOutboundAnchorObservedDocuments(t)
	documents := receiver.(documentVault)
	sets := []OutboundAnchorSet{
		outboundAnchorPublicationGroupSet("00", "shared"),
		outboundAnchorPublicationGroupSet("01", "shared"),
		outboundAnchorPublicationGroupSet("02", "shared"),
	}
	targetURL := sets[0].Anchors[0].TargetURL
	if _, err := receiver.Receive(
		t.Context(),
		[]Document{{NormalizedURL: targetURL}},
	); err != nil {
		t.Fatal(err)
	}
	engine.clearObservations()
	reservation, err := documents.ReserveDocumentLineages(
		t.Context(),
		outboundAnchorPublicationSourceURLs(sets),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer documents.ReleaseDocumentLineages(reservation)
	wantFailure := errors.New("second publication group failed")
	engine.failPublicationOnce(sets[1].SourceURL, wantFailure)
	maximumEncodedBytes := outboundAnchorFinalizationEncodedByteCeiling(
		outboundAnchorPublicationGroupFinalizations(sets)[0],
	)
	_, err = documents.replaceReservedOutboundAnchorDocumentsWithin(
		t.Context(),
		reservation,
		sets,
		nil,
		maximumEncodedBytes,
	)
	if !errors.Is(err, wantFailure) {
		t.Fatalf("publication group failure = %v", err)
	}
	assertOutboundAnchorPublicationTargetTotals(t, documents, sets, []int{1, 0, 0})
	assertOutboundAnchorAggregatedTargetMutation(
		t,
		directory,
		engine,
		targetURL,
		len(sets),
	)
	engine.clearFailure()
	if _, err := documents.replaceReservedOutboundAnchorDocumentsWithin(
		t.Context(),
		reservation,
		sets,
		nil,
		maximumEncodedBytes,
	); err != nil {
		t.Fatalf("publication group replay: %v", err)
	}
	assertOutboundAnchorPublicationReplay(t, directory, documents, sets, targetURL)
}

func TestOutboundAnchorPublicationGroupsRequireLineageBetweenTransactions(t *testing.T) {
	_, receiver, engine := openOutboundAnchorObservedDocuments(t)
	documents := receiver.(documentVault)
	sets := []OutboundAnchorSet{
		outboundAnchorPublicationGroupSet("00", "shared-lineage"),
		outboundAnchorPublicationGroupSet("01", "shared-lineage"),
	}
	sources := []string{sets[0].SourceURL, sets[1].SourceURL}
	reservation, err := documents.ReserveDocumentLineages(t.Context(), sources)
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
		_, publication := observation.buckets[outboundAnchorPublicationBucket]
		if publication && !released {
			documents.ReleaseDocumentLineages(reservation)
			released = true
		}
	})
	_, err = documents.replaceReservedOutboundAnchorDocumentsWithin(
		t.Context(),
		reservation,
		sets,
		nil,
		outboundAnchorFinalizationEncodedByteCeiling(
			outboundAnchorPublicationGroupFinalizations(sets)[0],
		),
	)
	if err == nil || !released {
		t.Fatalf("released lineage between publication transactions = %t/%v", released, err)
	}
	for index, set := range sets {
		publication := readOutboundAnchorReplacementPublication(
			t,
			documents,
			set.SourceURL,
		)
		wantTargets := 0
		if index == 0 {
			wantTargets = 1
		}
		if len(publication.Targets) != wantTargets {
			t.Fatalf(
				"publication %d after lineage release = %#v",
				index,
				publication.Targets,
			)
		}
	}
}

func TestOutboundAnchorPublicationGroupsRetainAllSourceLineages(t *testing.T) {
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	sets := []OutboundAnchorSet{
		outboundAnchorPublicationGroupSet("00", "00"),
		outboundAnchorPublicationGroupSet("01", "01"),
	}
	sources := []string{sets[0].SourceURL, sets[1].SourceURL}
	stored := []Document{
		{NormalizedURL: sets[0].Anchors[0].TargetURL},
		{NormalizedURL: sets[1].Anchors[0].TargetURL},
	}
	if _, err := receiver.Receive(t.Context(), stored); err != nil {
		t.Fatal(err)
	}
	reservation, err := documents.ReserveDocumentLineages(t.Context(), sources)
	if err != nil {
		t.Fatal(err)
	}
	reservationReleased := false
	t.Cleanup(func() {
		if !reservationReleased {
			documents.ReleaseDocumentLineages(reservation)
		}
	})
	maximumEncodedBytes := outboundAnchorFinalizationEncodedByteCeiling(
		outboundAnchorPublicationGroupFinalizations(sets)[0],
	)
	firstEntered, releaseFirst, firstDone := startPausedOutboundAnchorReplacement(
		t.Context(),
		documents,
		reservation,
		sets,
		maximumEncodedBytes,
	)
	requireOutboundAnchorSignal(t, firstEntered, "first publication group did not reach projection")
	competingTarget := "https://target.example/competing"
	secondDone := startOutboundAnchorSourceReplacement(
		t.Context(),
		receiver.(OutboundAnchorDocumentReplacer),
		sets[1].SourceURL,
		competingTarget,
	)
	requireOutboundAnchorOperationWaiting(t, secondDone)
	close(releaseFirst)
	requireOutboundAnchorOperation(t, firstDone, "byte-adaptive replacement")
	documents.ReleaseDocumentLineages(reservation)
	reservationReleased = true
	requireOutboundAnchorOperation(t, secondDone, "competing source writer")
	publication := readOutboundAnchorReplacementPublication(
		t,
		documents,
		sets[1].SourceURL,
	)
	if !slices.Equal(publication.Targets, []string{competingTarget}) {
		t.Fatalf("competing publication = %#v", publication.Targets)
	}
}

func outboundAnchorMaximumLongPublicationSets() []OutboundAnchorSet {
	sets := make(
		[]OutboundAnchorSet,
		0,
		MaximumOutboundAnchorSourcesPerReplacement,
	)
	for source := range MaximumOutboundAnchorSourcesPerReplacement {
		anchors := make([]OutboundAnchor, 0, maximumOutboundAnchors)
		for edge := range maximumOutboundAnchors {
			anchors = append(anchors, OutboundAnchor{
				TargetURL: outboundAnchorLongTargetURL(source, edge),
				Text:      "anchor",
			})
		}
		sets = append(sets, OutboundAnchorSet{
			SourceURL: fmt.Sprintf("https://source.example/%02d", source),
			Anchors:   anchors,
		})
	}

	return sets
}

func requireBoundedOutboundAnchorPublicationGroups(
	t *testing.T,
	finalizations []OutboundAnchorFinalization,
	sets []OutboundAnchorSet,
) [][]OutboundAnchorFinalization {
	t.Helper()
	conservativeEncodedBytes, actualTargetBytes := outboundAnchorPublicationByteEvidence(
		finalizations,
	)
	if conservativeEncodedBytes <= outboundAnchorPublicationMaximumEncodedBytes ||
		actualTargetBytes >= outboundAnchorPublicationMaximumEncodedBytes {
		t.Fatalf(
			"publication conservative/actual bytes = %d/%d",
			conservativeEncodedBytes,
			actualTargetBytes,
		)
	}
	groups, err := outboundAnchorPublicationGroups(
		finalizations,
		outboundAnchorPublicationMaximumEncodedBytes,
	)
	if err != nil || len(groups) < 2 {
		t.Fatalf("adaptive publication groups = %d/%v", len(groups), err)
	}
	gotSources := boundedOutboundAnchorPublicationSources(t, groups)
	wantSources := make([]string, 0, len(sets))
	for _, set := range sets {
		wantSources = append(wantSources, set.SourceURL)
	}
	if !slices.Equal(gotSources, wantSources) {
		t.Fatal("adaptive publication groups changed canonical source order")
	}

	return groups
}

func outboundAnchorPublicationByteEvidence(
	finalizations []OutboundAnchorFinalization,
) (int, int) {
	conservativeEncodedBytes := 0
	actualTargetBytes := 0
	for _, finalization := range finalizations {
		conservativeEncodedBytes += outboundAnchorFinalizationEncodedByteCeiling(finalization)
		actualTargetBytes += len(finalization.sourceURL)
		for _, targetURL := range finalization.desired.Targets {
			actualTargetBytes += len(targetURL)
		}
	}

	return conservativeEncodedBytes, actualTargetBytes
}

func boundedOutboundAnchorPublicationSources(
	t *testing.T,
	groups [][]OutboundAnchorFinalization,
) []string {
	t.Helper()
	sources := make([]string, 0, MaximumOutboundAnchorSourcesPerReplacement)
	for _, group := range groups {
		if len(group) > MaximumOutboundAnchorSourcesPerReplacement {
			t.Fatalf("adaptive publication group sources = %d", len(group))
		}
		groupEncodedBytes := 0
		for _, finalization := range group {
			sources = append(sources, finalization.sourceURL)
			groupEncodedBytes += outboundAnchorFinalizationEncodedByteCeiling(finalization)
		}
		if groupEncodedBytes > outboundAnchorPublicationMaximumEncodedBytes {
			t.Fatalf("adaptive publication group bytes = %d", groupEncodedBytes)
		}
	}

	return sources
}

func assertOutboundAnchorReplacementTransactionBounds(
	t *testing.T,
	observations []outboundAnchorTransactionObservation,
	wantPublications int,
) {
	t.Helper()
	publications := 0
	for _, observation := range observations {
		if _, publication := observation.buckets[outboundAnchorPublicationBucket]; !publication {
			continue
		}
		publications++
		if observation.rows > MaximumOutboundAnchorSourcesPerReplacement ||
			observation.encodedBytes > outboundAnchorPublicationMaximumEncodedBytes {
			t.Fatalf("publication transaction = %#v", observation)
		}
	}
	if publications != wantPublications {
		t.Fatalf("publication transactions = %d, want %d", publications, wantPublications)
	}
	for _, observation := range outboundAnchorMutationObservations(observations) {
		if len(observation.targets) > outboundAnchorTargetPageSize ||
			observation.rows > outboundAnchorMutationMaximumRows ||
			observation.encodedBytes > outboundAnchorMutationMaximumEncodedBytes {
			t.Fatalf("target transaction = %#v", observation)
		}
	}
}

func assertOutboundAnchorMaximumPublications(
	t *testing.T,
	documents documentVault,
	sets []OutboundAnchorSet,
) {
	t.Helper()
	for _, set := range sets {
		publication := readOutboundAnchorReplacementPublication(
			t,
			documents,
			set.SourceURL,
		)
		if len(publication.Targets) != maximumOutboundAnchors {
			t.Fatalf(
				"source %s publication targets = %d",
				set.SourceURL,
				len(publication.Targets),
			)
		}
	}
}

func outboundAnchorPublicationSourceURLs(sets []OutboundAnchorSet) []string {
	sources := make([]string, 0, len(sets))
	for _, set := range sets {
		sources = append(sources, set.SourceURL)
	}

	return sources
}

func assertOutboundAnchorPublicationTargetTotals(
	t *testing.T,
	documents documentVault,
	sets []OutboundAnchorSet,
	want []int,
) {
	t.Helper()
	for index, set := range sets {
		publication := readOutboundAnchorReplacementPublication(
			t,
			documents,
			set.SourceURL,
		)
		if len(publication.Targets) != want[index] {
			t.Fatalf(
				"publication %d targets = %d, want %d",
				index,
				len(publication.Targets),
				want[index],
			)
		}
	}
}

func assertOutboundAnchorAggregatedTargetMutation(
	t *testing.T,
	directory DocumentDirectory,
	engine *outboundAnchorObservingEngine,
	targetURL string,
	wantInlinks int,
) {
	t.Helper()
	mutationTransactions := 0
	for _, observation := range engine.observations() {
		if _, mutated := observation.buckets[inboundAnchorBucket]; !mutated {
			continue
		}
		mutationTransactions++
		if !slices.Equal(observation.targets, []string{targetURL}) ||
			observation.puts[inboundAnchorBucket] != 1 ||
			observation.puts[orderedDocumentBucketName] != 1 {
			t.Fatalf("aggregated target transaction = %#v", observation)
		}
	}
	targetReads := engine.getTotal(inboundAnchorBucket, targetURL)
	if mutationTransactions != 1 || targetReads != 1 {
		t.Fatalf(
			"aggregated target transactions/reads = %d/%d",
			mutationTransactions,
			targetReads,
		)
	}
	document, found, err := directory.Document(t.Context(), targetURL)
	if err != nil || !found || len(document.Inlinks) != wantInlinks {
		t.Fatalf(
			"aggregated target after publication failure = %#v/%t/%v",
			document,
			found,
			err,
		)
	}
}

func assertOutboundAnchorPublicationReplay(
	t *testing.T,
	directory DocumentDirectory,
	documents documentVault,
	sets []OutboundAnchorSet,
	targetURL string,
) {
	t.Helper()
	for index, set := range sets {
		publication := readOutboundAnchorReplacementPublication(
			t,
			documents,
			set.SourceURL,
		)
		if !slices.Equal(publication.Targets, []string{targetURL}) {
			t.Fatalf("publication %d after replay = %#v", index, publication.Targets)
		}
	}
	document, found, err := directory.Document(t.Context(), targetURL)
	if err != nil || !found || len(document.Inlinks) != len(sets) {
		t.Fatalf("target after replay = %#v/%t/%v", document, found, err)
	}
}

func startPausedOutboundAnchorReplacement(
	ctx context.Context,
	documents documentVault,
	reservation DocumentLineageReservation,
	sets []OutboundAnchorSet,
	maximumEncodedBytes int,
) (<-chan struct{}, chan<- struct{}, <-chan error) {
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := documents.replaceReservedOutboundAnchorDocumentsWithin(
			ctx,
			reservation,
			sets,
			func([]Document) error {
				close(entered)
				<-release

				return nil
			},
			maximumEncodedBytes,
		)
		done <- err
	}()

	return entered, release, done
}

func startOutboundAnchorSourceReplacement(
	ctx context.Context,
	replacer OutboundAnchorDocumentReplacer,
	sourceURL string,
	targetURL string,
) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := replacer.ReplaceOutboundAnchorDocuments(
			ctx,
			[]OutboundAnchorSet{{
				SourceURL: sourceURL,
				Anchors: []OutboundAnchor{{
					TargetURL: targetURL,
					Text:      "competing",
				}},
			}},
			nil,
		)
		done <- err
	}()

	return done
}

func requireOutboundAnchorSignal(
	t *testing.T,
	signal <-chan struct{},
	failure string,
) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}

func requireOutboundAnchorOperationWaiting(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("later-group source lineage was not retained: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
}

func requireOutboundAnchorOperation(
	t *testing.T,
	done <-chan error,
	operation string,
) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
	case <-time.After(time.Second):
		t.Fatalf("%s did not finish", operation)
	}
}

func outboundAnchorLongTargetURL(source int, edge int) string {
	prefix := fmt.Sprintf("https://target.example/%02d/%04d/", source, edge)

	return prefix + strings.Repeat("x", 700-len(prefix))
}

func outboundAnchorPublicationGroupSet(source string, target string) OutboundAnchorSet {
	return OutboundAnchorSet{
		SourceURL: "https://source.example/" + source,
		Anchors: []OutboundAnchor{{
			TargetURL: "https://target.example/" + target,
			Text:      "anchor",
		}},
	}
}

func outboundAnchorPublicationGroupFinalizations(
	sets []OutboundAnchorSet,
) []OutboundAnchorFinalization {
	finalizations := make([]OutboundAnchorFinalization, 0, len(sets))
	for _, set := range sets {
		anchors, targets := canonicalOutboundAnchors(set.SourceURL, set.Anchors)
		finalizations = append(finalizations, OutboundAnchorFinalization{
			sourceURL: set.SourceURL,
			desired:   desiredOutboundAnchorPublication(anchors, targets),
		})
	}

	return finalizations
}
