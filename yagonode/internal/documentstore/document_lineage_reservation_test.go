package documentstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestReservedOutboundAnchorsRequireActiveCoveredLineage(t *testing.T) {
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/page"
	target := "https://target.example/page"
	reservation, err := documents.ReserveDocumentLineages(
		t.Context(),
		[]string{"  " + source + "  ", source},
	)
	if err != nil {
		t.Fatalf("reserve lineages: %v", err)
	}
	update, err := documents.ReplaceReservedOutboundAnchors(
		t.Context(),
		reservation,
		[]OutboundAnchorSet{{
			SourceURL: source,
			Anchors:   []OutboundAnchor{{TargetURL: target}},
		}},
	)
	if err != nil {
		t.Fatalf("replace reserved anchors: %v", err)
	}
	if err := documents.FinalizeOutboundAnchors(t.Context(), update.Finalizations); err != nil {
		t.Fatalf("finalize reserved anchors: %v", err)
	}
	secondAcquired := make(chan DocumentLineageReservation, 1)
	go func() {
		second, err := documents.ReserveDocumentLineages(t.Context(), []string{source})
		if err != nil {
			secondAcquired <- nil

			return
		}
		secondAcquired <- second
	}()
	select {
	case <-secondAcquired:
		t.Fatal("anchor finalization released the outer document lineage")
	case <-time.After(25 * time.Millisecond):
	}
	documents.ReleaseDocumentLineages(reservation)
	select {
	case second := <-secondAcquired:
		if second == nil {
			t.Fatal("second reservation failed")
		}
		documents.ReleaseDocumentLineages(second)
	case <-time.After(time.Second):
		t.Fatal("second reservation remained blocked")
	}
	if _, err := documents.ReplaceReservedOutboundAnchors(
		t.Context(),
		reservation,
		[]OutboundAnchorSet{{SourceURL: source}},
	); err == nil {
		t.Fatal("released lineage reservation was accepted")
	}
}

func TestReservedOutboundAnchorsRejectWrongOwnerAndUncoveredSource(t *testing.T) {
	_, firstReceiver := openDocuments(t)
	_, secondReceiver := openDocuments(t)
	first := firstReceiver.(documentVault)
	second := secondReceiver.(documentVault)
	reservation, err := first.ReserveDocumentLineages(
		t.Context(),
		[]string{"https://source.example/covered"},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer first.ReleaseDocumentLineages(reservation)
	for _, test := range []struct {
		name      string
		documents documentVault
		sets      []OutboundAnchorSet
	}{
		{
			name:      "wrong owner",
			documents: second,
			sets: []OutboundAnchorSet{{
				SourceURL: "https://source.example/covered",
			}},
		},
		{
			name:      "uncovered source",
			documents: first,
			sets: []OutboundAnchorSet{{
				SourceURL: "https://source.example/uncovered",
			}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.documents.ReplaceReservedOutboundAnchors(
				t.Context(),
				reservation,
				test.sets,
			); err == nil {
				t.Fatal("invalid lineage reservation was accepted")
			}
		})
	}
	if _, err := first.ReplaceReservedOutboundAnchors(
		t.Context(),
		nil,
		nil,
	); err == nil {
		t.Fatal("nil lineage reservation was accepted")
	}
	tooMany := make([]OutboundAnchorSet, MaximumOutboundAnchorSourcesPerReplacement+1)
	for index := range tooMany {
		tooMany[index].SourceURL = "https://source.example/" + strings.Repeat("x", index+1)
	}
	if _, err := first.ReplaceReservedOutboundAnchors(
		t.Context(),
		reservation,
		tooMany,
	); err == nil {
		t.Fatal("reserved anchor source limit was ignored")
	}
}

func TestDocumentLineageReservationCancellationLeavesSourceAvailable(t *testing.T) {
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/cancel"
	first, err := documents.ReserveDocumentLineages(t.Context(), []string{source})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := documents.ReserveDocumentLineages(ctx, []string{source})
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("reservation returned before cancellation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("reservation cancellation = %v", err)
	}
	documents.ReleaseDocumentLineages(first)
	last, err := documents.ReserveDocumentLineages(t.Context(), []string{source})
	if err != nil {
		t.Fatalf("reserve after cancellation: %v", err)
	}
	documents.ReleaseDocumentLineages(last)
}

func TestDocumentLineageReservationsUseOneOrderForMultipleSources(t *testing.T) {
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	sources := distinctStoredDocumentBoundaryURLs(
		documents.outboundBoundaries,
		2,
		"https://sources.example/",
	)
	first, err := documents.ReserveDocumentLineages(
		t.Context(),
		[]string{sources[0], sources[1]},
	)
	if err != nil {
		t.Fatal(err)
	}
	secondAcquired := make(chan DocumentLineageReservation, 1)
	go func() {
		second, err := documents.ReserveDocumentLineages(
			t.Context(),
			[]string{sources[1], sources[0], sources[1]},
		)
		if err != nil {
			secondAcquired <- nil

			return
		}
		secondAcquired <- second
	}()
	select {
	case <-secondAcquired:
		t.Fatal("reverse reservation crossed active sources")
	case <-time.After(25 * time.Millisecond):
	}
	documents.ReleaseDocumentLineages(first)
	select {
	case second := <-secondAcquired:
		if second == nil {
			t.Fatal("reverse reservation failed")
		}
		documents.ReleaseDocumentLineages(second)
	case <-time.After(time.Second):
		t.Fatal("reverse reservation deadlocked")
	}
}

func TestDocumentLineageReservationRejectsInvalidAndForeignRelease(t *testing.T) {
	_, firstReceiver := openDocuments(t)
	_, secondReceiver := openDocuments(t)
	first := firstReceiver.(documentVault)
	second := secondReceiver.(documentVault)
	if _, err := first.ReserveDocumentLineages(
		t.Context(),
		[]string{" ", strings.Repeat("x", yagomodel.MaximumURLIdentityBytes+1)},
	); err == nil {
		t.Fatal("invalid document lineage URL was reserved")
	}
	reservation, err := first.ReserveDocumentLineages(
		t.Context(),
		[]string{"https://source.example/"},
	)
	if err != nil {
		t.Fatal(err)
	}
	first.ReleaseDocumentLineages(nil)
	second.ReleaseDocumentLineages(reservation)
	if err := first.activeDocumentLineageLease(
		reservation,
		[]string{strings.Repeat("x", yagomodel.MaximumURLIdentityBytes+1)},
	); err == nil {
		t.Fatal("invalid covered source was accepted")
	}
	first.ReleaseDocumentLineages(reservation)
	(*documentLineageLease)(nil).close()
}

func TestCanonicalReservedDocumentsValidatesAndReadsReservedSources(t *testing.T) {
	_, firstReceiver := openDocuments(t)
	_, secondReceiver := openDocuments(t)
	first := firstReceiver.(documentVault)
	second := secondReceiver.(documentVault)
	source := "https://source.example/"
	reservation, err := first.ReserveDocumentLineages(
		t.Context(),
		[]string{" ", source, source},
	)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := first.CanonicalReservedDocuments(
		t.Context(),
		reservation,
		[]Document{{NormalizedURL: source, Title: "source"}, {}},
	)
	if err != nil || len(canonical) != 1 || canonical[0].Title != "source" {
		t.Fatalf("canonical reserved documents = %#v, %v", canonical, err)
	}
	for _, test := range []struct {
		name        string
		documents   documentVault
		reservation DocumentLineageReservation
		url         string
	}{
		{
			name:        "nil reservation",
			documents:   first,
			reservation: nil,
			url:         source,
		},
		{
			name:        "wrong owner",
			documents:   second,
			reservation: reservation,
			url:         source,
		},
		{
			name:        "uncovered source",
			documents:   first,
			reservation: reservation,
			url:         "https://uncovered.example/",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.documents.CanonicalReservedDocuments(
				t.Context(),
				test.reservation,
				[]Document{{NormalizedURL: test.url}},
			); err == nil {
				t.Fatal("invalid reservation was accepted")
			}
		})
	}
	first.ReleaseDocumentLineages(reservation)
	if _, err := first.CanonicalReservedDocuments(
		t.Context(),
		reservation,
		[]Document{{NormalizedURL: source}},
	); err == nil {
		t.Fatal("released reservation was accepted")
	}
}

func TestCanonicalReservedDocumentsReportsLoopAndStoredEvidenceFailures(t *testing.T) {
	_, documents, engine := openDocumentStorageFaultVault(t)
	source := "https://source.example/"
	reservation, err := documents.ReserveDocumentLineages(
		t.Context(),
		[]string{source},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer documents.ReleaseDocumentLineages(reservation)
	delayed := &errAfterContext{
		Context:   t.Context(),
		remaining: 2,
		err:       context.Canceled,
	}
	if _, err := documents.CanonicalReservedDocuments(
		delayed,
		reservation,
		[]Document{{NormalizedURL: source}},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("canonical loop cancellation = %v", err)
	}
	malformed := "https://source.example/malformed"
	malformedReservation, err := documents.ReserveDocumentLineages(
		t.Context(),
		[]string{malformed},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer documents.ReleaseDocumentLineages(malformedReservation)
	engine.putRaw(documentLocationBucketName, vault.Key(malformed), []byte{1})
	if _, err := documents.CanonicalReservedDocuments(
		t.Context(),
		malformedReservation,
		[]Document{{NormalizedURL: malformed}},
	); err == nil {
		t.Fatal("malformed reserved document location was accepted")
	}
}

func TestCompatibilityAnchorReplacementReleasesReservationOnInternalFailure(t *testing.T) {
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/"
	sets := []OutboundAnchorSet{{SourceURL: source}}
	released, err := documents.ReserveDocumentLineages(t.Context(), []string{source})
	if err != nil {
		t.Fatal(err)
	}
	documents.ReleaseDocumentLineages(released)
	if _, err := documents.replaceReservedOutboundAnchors(
		t.Context(),
		released,
		sets,
		true,
	); err == nil {
		t.Fatal("released compatibility reservation was accepted")
	}
	active, err := documents.ReserveDocumentLineages(t.Context(), []string{source})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := documents.replaceReservedOutboundAnchors(
		ctx,
		active,
		sets,
		true,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("compatibility cancellation = %v", err)
	}
	if err := documents.activeDocumentLineageLease(active, []string{source}); err == nil {
		t.Fatal("failed compatibility replacement retained its reservation")
	}
}
