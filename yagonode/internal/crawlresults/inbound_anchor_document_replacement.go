package crawlresults

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (c *IngestConsumer) replaceOutboundAnchorDocuments(
	ctx context.Context,
	deliveries []IngestDelivery,
	sets []documentstore.OutboundAnchorSet,
	reservation documentstore.DocumentLineageReservation,
) (bool, bool) {
	var receipt documentstore.AnchorReplacementReceipt
	var err error
	switch reservation {
	case nil:
		replacer, ok := c.anchors.(documentstore.OutboundAnchorDocumentReplacer)
		if !ok {
			return false, false
		}
		receipt, err = replacer.ReplaceOutboundAnchorDocuments(ctx, sets)
	default:
		replacer, ok := c.reservedAnchors.(documentstore.ReservedOutboundAnchorDocumentReplacer)
		if !ok {
			return false, false
		}
		receipt, err = replacer.ReplaceReservedOutboundAnchorDocuments(
			ctx,
			reservation,
			sets,
		)
	}
	if err != nil {
		c.redeliverGroup(ctx, deliveries, "inbound anchor store", err)

		return true, true
	}
	if receipt.Busy {
		c.redeliverGroup(ctx, deliveries, "inbound anchor store at capacity", nil)

		return true, true
	}

	return false, true
}
