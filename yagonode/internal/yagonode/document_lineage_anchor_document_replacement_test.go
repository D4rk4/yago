package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type lineageAnchorDocumentScript struct {
	*lineageAnchorScript
	receipt        documentstore.AnchorReplacementReceipt
	replacementErr error
	documentCalls  int
	reservedCalls  int
}

func (s *lineageAnchorDocumentScript) ReplaceOutboundAnchorDocuments(
	_ context.Context,
	_ []documentstore.OutboundAnchorSet,
) (documentstore.AnchorReplacementReceipt, error) {
	s.documentCalls++

	return s.receipt, s.replacementErr
}

func (s *lineageAnchorDocumentScript) ReplaceReservedOutboundAnchors(
	context.Context,
	documentstore.DocumentLineageReservation,
	[]documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	return documentstore.AnchorUpdate{}, errors.New("legacy reserved replacement called")
}

func (s *lineageAnchorDocumentScript) ReplaceReservedOutboundAnchorDocuments(
	_ context.Context,
	_ documentstore.DocumentLineageReservation,
	_ []documentstore.OutboundAnchorSet,
) (documentstore.AnchorReplacementReceipt, error) {
	s.reservedCalls++

	return s.receipt, s.replacementErr
}

func TestDocumentLineageEvictionUsesPagedAnchorDocumentReplacement(t *testing.T) {
	anchors := &lineageAnchorDocumentScript{lineageAnchorScript: &lineageAnchorScript{}}
	index := &lineageBatchIndexScript{}
	evictor := documentLineageEvictor{anchors: anchors, index: index}
	if err := evictor.clearOutboundAnchorContributions(
		t.Context(),
		nil,
		"https://source.example/",
	); err != nil {
		t.Fatal(err)
	}
	if anchors.documentCalls != 1 || anchors.finalizeCalls != 0 || index.batches != 0 {
		t.Fatalf("replacement = %#v, index = %#v", anchors, index)
	}
}

func TestDocumentLineageAnchorDocumentReplacementSurfacesFailures(t *testing.T) {
	wantErr := errors.New("replacement failed")
	anchors := &lineageAnchorDocumentScript{
		lineageAnchorScript: &lineageAnchorScript{},
		replacementErr:      wantErr,
	}
	evictor := documentLineageEvictor{anchors: anchors}
	if err := evictor.clearOutboundAnchorContributions(
		t.Context(),
		nil,
		"https://source.example/",
	); !errors.Is(err, wantErr) {
		t.Fatalf("replacement error = %v, want %v", err, wantErr)
	}
	anchors.replacementErr = nil
	anchors.receipt.Busy = true
	if err := evictor.clearOutboundAnchorContributions(
		t.Context(),
		nil,
		"https://source.example/",
	); err == nil {
		t.Fatal("capacity result was accepted")
	}
}

func TestReservedDocumentLineageEvictionUsesPagedAnchorReplacement(t *testing.T) {
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
	anchors := &lineageAnchorDocumentScript{lineageAnchorScript: &lineageAnchorScript{}}
	evictor := documentLineageEvictor{
		anchors:         anchors,
		reservedAnchors: anchors,
	}
	if err := evictor.clearOutboundAnchorContributions(
		t.Context(),
		reservation,
		"https://source.example/",
	); err != nil {
		t.Fatal(err)
	}
	if anchors.reservedCalls != 1 || anchors.finalizeCalls != 0 {
		t.Fatalf("reserved replacement = %#v", anchors)
	}
}
