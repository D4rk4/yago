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
	visit func([]documentstore.Document) error,
) (documentstore.AnchorReplacementReceipt, error) {
	s.documentCalls++
	if s.replacementErr != nil {
		return documentstore.AnchorReplacementReceipt{}, s.replacementErr
	}
	if len(s.documents) > 0 {
		if err := visit(s.documents); err != nil {
			return documentstore.AnchorReplacementReceipt{}, err
		}
	}

	return s.receipt, nil
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
	visit func([]documentstore.Document) error,
) (documentstore.AnchorReplacementReceipt, error) {
	s.reservedCalls++
	if s.replacementErr != nil {
		return documentstore.AnchorReplacementReceipt{}, s.replacementErr
	}
	if len(s.documents) > 0 {
		if err := visit(s.documents); err != nil {
			return documentstore.AnchorReplacementReceipt{}, err
		}
	}

	return s.receipt, nil
}

func TestDocumentLineageEvictionUsesPagedAnchorDocumentReplacement(t *testing.T) {
	target := documentstore.Document{NormalizedURL: "https://target.example/"}
	anchors := &lineageAnchorDocumentScript{
		lineageAnchorScript: &lineageAnchorScript{documents: []documentstore.Document{target}},
	}
	index := &lineageBatchIndexScript{}
	evictor := documentLineageEvictor{anchors: anchors, index: index}
	if err := evictor.clearOutboundAnchorContributions(
		t.Context(),
		nil,
		"https://source.example/",
	); err != nil {
		t.Fatal(err)
	}
	if anchors.documentCalls != 1 || anchors.finalizeCalls != 0 || index.batches != 1 ||
		len(index.docs) != 1 || index.docs[0].NormalizedURL != target.NormalizedURL {
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
	anchors.receipt.Busy = false
	anchors.documents = []documentstore.Document{{NormalizedURL: "https://target.example/"}}
	indexErr := errors.New("index failed")
	evictor.index = &lineageBatchIndexScript{lineageIndexScript: lineageIndexScript{err: indexErr}}
	if err := evictor.clearOutboundAnchorContributions(
		t.Context(),
		nil,
		"https://source.example/",
	); !errors.Is(err, indexErr) {
		t.Fatalf("index error = %v, want %v", err, indexErr)
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
