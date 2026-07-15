package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type failingDocumentLineageReserver struct {
	err error
}

func (f failingDocumentLineageReserver) ReserveDocumentLineages(
	context.Context,
	[]string,
) (documentstore.DocumentLineageReservation, error) {
	return nil, f.err
}

func (failingDocumentLineageReserver) ReleaseDocumentLineages(
	documentstore.DocumentLineageReservation,
) {
}

func TestDocumentEvictionReservationCanonicalizesCoverageAndLifecycle(t *testing.T) {
	scope, err := (documentLineageEvictor{
		documents: &lineageDocumentScript{docs: map[string]documentstore.Document{}},
	}).ReserveDocumentEvictions(
		t.Context(),
		[]string{" ", " https://source.example/ ", "https://source.example/"},
	)
	if err != nil {
		t.Fatal(err)
	}
	reservation := scope.(*reservedDocumentLineageEviction)
	if len(reservation.normalizedURLs) != 1 {
		t.Fatalf("reserved URLs = %v", reservation.normalizedURLs)
	}
	if removed, err := reservation.Delete(
		t.Context(),
		" https://source.example/ ",
	); err != nil || removed {
		t.Fatalf("covered deletion = %t, %v", removed, err)
	}
	if _, err := reservation.Delete(
		t.Context(),
		"https://uncovered.example/",
	); err == nil {
		t.Fatal("uncovered deletion was accepted")
	}
	reservation.Release()
	reservation.Release()
	if _, err := reservation.Delete(
		t.Context(),
		"https://source.example/",
	); err == nil {
		t.Fatal("released reservation was accepted")
	}
	var missing *reservedDocumentLineageEviction
	if _, err := missing.Delete(t.Context(), "https://source.example/"); err == nil {
		t.Fatal("nil reservation was accepted")
	}
	missing.Release()
}

func TestDocumentEvictionReservationSurfacesLineageFailure(t *testing.T) {
	wantErr := errors.New("reservation failed")
	evictor := documentLineageEvictor{
		lineages: failingDocumentLineageReserver{err: wantErr},
	}
	if _, err := evictor.ReserveDocumentEvictions(
		t.Context(),
		[]string{"https://source.example/"},
	); !errors.Is(err, wantErr) {
		t.Fatalf("reservation error = %v, want %v", err, wantErr)
	}
	if _, err := evictor.Delete(
		t.Context(),
		"https://source.example/",
	); !errors.Is(err, wantErr) {
		t.Fatalf("delete reservation error = %v, want %v", err, wantErr)
	}
}

func TestDocumentLineageAnchorEvictionRequiresScopedReceiver(t *testing.T) {
	storage := openTestVault(t)
	_, receiver, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	lineages := receiver.(documentstore.DocumentLineageReserver)
	reservation, err := lineages.ReserveDocumentLineages(
		t.Context(),
		[]string{"https://source.example/"},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer lineages.ReleaseDocumentLineages(reservation)
	evictor := documentLineageEvictor{anchors: &lineageAnchorScript{}}
	if err := evictor.clearOutboundAnchorContributions(
		t.Context(),
		reservation,
		"https://source.example/",
	); err == nil {
		t.Fatal("missing scoped anchor receiver was accepted")
	}
}

func TestDocumentLineageAnchorIndexCoversBatchAndSingleFailures(t *testing.T) {
	document := documentstore.Document{NormalizedURL: "https://target.example/"}
	anchors := &lineageAnchorScript{documents: []documentstore.Document{document}}
	batch := &lineageBatchIndexScript{}
	evictor := documentLineageEvictor{anchors: anchors, index: batch}
	if err := evictor.indexOutboundAnchorContributions(
		t.Context(),
		[]documentstore.OutboundAnchorFinalization{{}},
	); err != nil || batch.batches != 1 {
		t.Fatalf("batch anchor index = %d, %v", batch.batches, err)
	}
	wantErr := errors.New("index failed")
	batch.err = wantErr
	if err := evictor.indexOutboundAnchorContributions(
		t.Context(),
		[]documentstore.OutboundAnchorFinalization{{}},
	); !errors.Is(err, wantErr) {
		t.Fatalf("batch anchor index error = %v, want %v", err, wantErr)
	}
	single := &lineageIndexScript{err: wantErr}
	evictor.index = single
	if err := evictor.indexOutboundAnchorContributions(
		t.Context(),
		[]documentstore.OutboundAnchorFinalization{{}},
	); !errors.Is(err, wantErr) {
		t.Fatalf("single anchor index error = %v, want %v", err, wantErr)
	}
	anchors.update = documentstore.AnchorUpdate{
		Finalizations: []documentstore.OutboundAnchorFinalization{{}},
	}
	if err := evictor.clearOutboundAnchorContributions(
		t.Context(),
		nil,
		"https://source.example/",
	); !errors.Is(err, wantErr) {
		t.Fatalf("anchor cleanup index error = %v, want %v", err, wantErr)
	}
}
