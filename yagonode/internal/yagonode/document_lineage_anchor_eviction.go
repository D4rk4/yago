package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (d documentLineageEvictor) deletedDocumentLineage(
	ctx context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	if d.directory == nil {
		return documentstore.Document{}, false, nil
	}
	document, found, err := d.directory.Document(ctx, normalizedURL)
	if err != nil {
		return documentstore.Document{}, false, fmt.Errorf(
			"read deleted document lineage: %w",
			err,
		)
	}

	return document, found, nil
}

func (d documentLineageEvictor) clearOutboundAnchorContributions(
	ctx context.Context,
	reservation documentstore.DocumentLineageReservation,
	normalizedURL string,
) error {
	if d.anchors == nil {
		return nil
	}
	sets := []documentstore.OutboundAnchorSet{{SourceURL: normalizedURL}}
	var update documentstore.AnchorUpdate
	var err error
	switch {
	case reservation == nil:
		update, err = d.anchors.ReplaceOutboundAnchors(ctx, sets)
	case d.reservedAnchors == nil:
		return fmt.Errorf("reserved outbound anchor receiver is unavailable")
	default:
		update, err = d.reservedAnchors.ReplaceReservedOutboundAnchors(
			ctx,
			reservation,
			sets,
		)
	}
	defer d.anchors.ReleaseOutboundAnchors(update.Finalizations)
	if err != nil {
		return fmt.Errorf("clear outbound anchor contributions: %w", err)
	}
	if update.Busy {
		return fmt.Errorf("clear outbound anchor contributions at capacity")
	}
	if err := d.indexOutboundAnchorContributions(ctx, update.Finalizations); err != nil {
		return err
	}
	if err := d.anchors.FinalizeOutboundAnchors(ctx, update.Finalizations); err != nil {
		return fmt.Errorf("finalize outbound anchor contributions: %w", err)
	}

	return nil
}

func (d documentLineageEvictor) indexOutboundAnchorContributions(
	ctx context.Context,
	finalizations []documentstore.OutboundAnchorFinalization,
) error {
	if d.index == nil {
		return nil
	}
	if err := d.anchors.VisitOutboundAnchorDocuments(
		ctx,
		finalizations,
		func(documents []documentstore.Document) error {
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
		},
	); err != nil {
		return fmt.Errorf("index outbound anchor contributions: %w", err)
	}

	return nil
}
