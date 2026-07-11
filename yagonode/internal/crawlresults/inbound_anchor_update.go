package crawlresults

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (c *IngestConsumer) updateInboundAnchors(
	ctx context.Context,
	deliveries []IngestDelivery,
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

	return c.replaceOutboundAnchors(ctx, deliveries, sets)
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
	)
}

func (c *IngestConsumer) replaceOutboundAnchors(
	ctx context.Context,
	deliveries []IngestDelivery,
	sets []documentstore.OutboundAnchorSet,
) bool {
	update, err := c.anchors.ReplaceOutboundAnchors(ctx, sets)
	if err != nil {
		c.redeliverGroup(ctx, deliveries, "inbound anchor store", err)

		return true
	}
	if update.Busy {
		c.redeliverGroup(ctx, deliveries, "inbound anchor store at capacity", nil)

		return true
	}
	if c.index == nil || len(update.Documents) == 0 {
		return false
	}
	if err := c.indexDocuments(ctx, update.Documents); err != nil {
		c.redeliverGroup(ctx, deliveries, "inbound anchor index", err)

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
