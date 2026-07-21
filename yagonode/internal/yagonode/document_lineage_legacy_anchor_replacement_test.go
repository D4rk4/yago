package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type legacyLineageAnchorReceiver struct {
	*lineageAnchorScript
	reservedCalls int
}

func (r *legacyLineageAnchorReceiver) ReplaceReservedOutboundAnchors(
	context.Context,
	documentstore.DocumentLineageReservation,
	[]documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	r.reservedCalls++

	return r.update, r.err
}

func TestDocumentLineageEvictionSupportsLegacyReservedAnchorReceiver(t *testing.T) {
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
	anchors := &legacyLineageAnchorReceiver{lineageAnchorScript: &lineageAnchorScript{}}
	evictor := documentLineageEvictor{anchors: anchors, reservedAnchors: anchors}
	if err := evictor.clearOutboundAnchorContributions(
		t.Context(),
		reservation,
		"https://source.example/",
	); err != nil {
		t.Fatal(err)
	}
	if anchors.reservedCalls != 1 || anchors.finalizeCalls != 1 {
		t.Fatalf(
			"legacy reserved replacement calls = %d, finalizations = %d",
			anchors.reservedCalls,
			anchors.finalizeCalls,
		)
	}
}
