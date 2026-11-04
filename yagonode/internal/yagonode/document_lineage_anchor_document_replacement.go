package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (d documentLineageEvictor) clearOutboundAnchorDocuments(
	ctx context.Context,
	reservation documentstore.DocumentLineageReservation,
	sets []documentstore.OutboundAnchorSet,
) (bool, error) {
	var receipt documentstore.AnchorReplacementReceipt
	var err error
	switch reservation {
	case nil:
		replacer, ok := d.anchors.(documentstore.OutboundAnchorDocumentReplacer)
		if !ok {
			return false, nil
		}
		receipt, err = replacer.ReplaceOutboundAnchorDocuments(ctx, sets)
	default:
		replacer, ok := d.reservedAnchors.(documentstore.ReservedOutboundAnchorDocumentReplacer)
		if !ok {
			return false, nil
		}
		receipt, err = replacer.ReplaceReservedOutboundAnchorDocuments(
			ctx,
			reservation,
			sets,
		)
	}
	if err != nil {
		return true, fmt.Errorf("clear outbound anchor contributions: %w", err)
	}
	if receipt.Busy {
		return true, fmt.Errorf("clear outbound anchor contributions at capacity")
	}

	return true, nil
}

func (d documentLineageEvictor) indexOutboundAnchorDocuments(
	ctx context.Context,
	documents []documentstore.Document,
) error {
	return indexAnchorDocuments(ctx, d.index, documents)
}
