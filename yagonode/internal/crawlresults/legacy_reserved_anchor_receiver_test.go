package crawlresults

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

type legacyReservedAnchorReceiver struct {
	outboundAnchorChunkScript
	reservedCalls int
}

func (r *legacyReservedAnchorReceiver) ReplaceReservedOutboundAnchors(
	context.Context,
	documentstore.DocumentLineageReservation,
	[]documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	r.reservedCalls++

	return documentstore.AnchorUpdate{}, nil
}

func TestLegacyReservedAnchorReceiverRemainsSupported(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	_, documents, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	sourceURL := "https://source.example/page"
	lineages := documents.(documentstore.DocumentLineageReserver)
	reservation, err := lineages.ReserveDocumentLineages(
		t.Context(),
		[]string{sourceURL},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer lineages.ReleaseDocumentLineages(reservation)

	receiver := &legacyReservedAnchorReceiver{}
	consumer := &IngestConsumer{anchors: receiver, reservedAnchors: receiver}
	deferred := consumer.replaceOutboundAnchors(
		t.Context(),
		nil,
		[]documentstore.OutboundAnchorSet{{SourceURL: sourceURL}},
		reservation,
	)
	if deferred || receiver.reservedCalls != 1 || len(receiver.groups) != 0 {
		t.Fatalf(
			"legacy reserved replacement = deferred %t, reserved %d, direct %d",
			deferred,
			receiver.reservedCalls,
			len(receiver.groups),
		)
	}
}
