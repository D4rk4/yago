package crawlresults

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (c *IngestConsumer) updateInboundAnchors(
	ctx context.Context,
	deliveries []IngestDelivery,
) bool {
	return c.updateReservedInboundAnchors(ctx, deliveries, nil)
}

func (c *IngestConsumer) updateReservedInboundAnchors(
	ctx context.Context,
	deliveries []IngestDelivery,
	reservation documentstore.DocumentLineageReservation,
) bool {
	if c.anchors == nil {
		return false
	}
	sets := make([]documentstore.OutboundAnchorSet, 0, len(deliveries))
	for _, delivery := range deliveries {
		if set, ok := outboundAnchorSetFromIngest(delivery.Batch.Document); ok {
			sets = append(sets, set)
		}
	}
	if len(sets) == 0 {
		return false
	}

	return c.replaceOutboundAnchors(ctx, deliveries, sets, reservation)
}

func (c *IngestConsumer) clearOutboundAnchors(
	ctx context.Context,
	delivery IngestDelivery,
) bool {
	if c.anchors == nil || delivery.Batch.SourceURL == "" {
		return false
	}

	return c.replaceOutboundAnchors(
		ctx,
		[]IngestDelivery{delivery},
		[]documentstore.OutboundAnchorSet{{SourceURL: delivery.Batch.SourceURL}},
		nil,
	)
}

func (c *IngestConsumer) replaceOutboundAnchors(
	ctx context.Context,
	deliveries []IngestDelivery,
	sets []documentstore.OutboundAnchorSet,
	reservation documentstore.DocumentLineageReservation,
) bool {
	var update documentstore.AnchorUpdate
	var err error
	switch {
	case reservation == nil:
		update, err = c.anchors.ReplaceOutboundAnchors(ctx, sets)
	case c.reservedAnchors == nil:
		err = fmt.Errorf("reserved outbound anchor receiver is unavailable")
	default:
		update, err = c.reservedAnchors.ReplaceReservedOutboundAnchors(
			ctx,
			reservation,
			sets,
		)
	}
	defer c.anchors.ReleaseOutboundAnchors(update.Finalizations)
	if err != nil {
		c.redeliverGroup(ctx, deliveries, "inbound anchor store", err)

		return true
	}
	if update.Busy {
		c.redeliverGroup(ctx, deliveries, "inbound anchor store at capacity", nil)

		return true
	}
	if c.index != nil {
		if err := c.anchors.VisitOutboundAnchorDocuments(
			ctx,
			update.Finalizations,
			func(documents []documentstore.Document) error {
				return c.indexDocuments(ctx, documents)
			},
		); err != nil {
			c.redeliverGroup(ctx, deliveries, "inbound anchor index", err)

			return true
		}
	}
	if err := c.anchors.FinalizeOutboundAnchors(ctx, update.Finalizations); err != nil {
		c.redeliverGroup(ctx, deliveries, "inbound anchor finalization", err)

		return true
	}

	return false
}

func outboundAnchorSetFromIngest(
	doc yagocrawlcontract.DocumentIngest,
) (documentstore.OutboundAnchorSet, bool) {
	if !doc.OutboundAnchorEvidenceKnown {
		return documentstore.OutboundAnchorSet{}, false
	}
	sourceURL := doc.NormalizedURL
	if sourceURL == "" {
		sourceURL = doc.CanonicalURL
	}
	if sourceURL == "" {
		return documentstore.OutboundAnchorSet{}, false
	}
	anchors := make([]documentstore.OutboundAnchor, 0, len(doc.OutboundAnchors))
	for _, anchor := range doc.OutboundAnchors {
		anchors = append(anchors, documentstore.OutboundAnchor{
			TargetURL:     anchor.TargetURL,
			Text:          anchor.Text,
			NoFollow:      anchor.NoFollow,
			UserGenerated: anchor.UserGenerated,
			Sponsored:     anchor.Sponsored,
		})
	}

	return documentstore.OutboundAnchorSet{SourceURL: sourceURL, Anchors: anchors}, true
}
