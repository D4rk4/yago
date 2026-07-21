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
	visit := func(documents []documentstore.Document) error {
		return d.indexOutboundAnchorDocuments(ctx, documents)
	}
	var receipt documentstore.AnchorReplacementReceipt
	var err error
	switch reservation {
	case nil:
		replacer, ok := d.anchors.(documentstore.OutboundAnchorDocumentReplacer)
		if !ok {
			return false, nil
		}
		receipt, err = replacer.ReplaceOutboundAnchorDocuments(ctx, sets, visit)
	default:
		replacer, ok := d.reservedAnchors.(documentstore.ReservedOutboundAnchorDocumentReplacer)
		if !ok {
			return false, nil
		}
		receipt, err = replacer.ReplaceReservedOutboundAnchorDocuments(
			ctx,
			reservation,
			sets,
			visit,
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
	if d.index == nil {
		return nil
	}
	if batch, ok := d.index.(documentBatchUpdateIndexer); ok {
		if err := batch.IndexBatch(ctx, documents); err != nil {
			return fmt.Errorf("index outbound anchor document batch: %w", err)
		}

		return nil
	}
	for _, document := range documents {
		if err := d.index.Index(ctx, document); err != nil {
			return fmt.Errorf("index outbound anchor document: %w", err)
		}
	}

	return nil
}
