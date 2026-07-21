package crawlresults

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (c *IngestConsumer) replaceOutboundAnchorChunks(
	ctx context.Context,
	deliveries []IngestDelivery,
	sets []documentstore.OutboundAnchorSet,
	reservation documentstore.DocumentLineageReservation,
) bool {
	for first := 0; first < len(sets); first += documentstore.MaximumOutboundAnchorSourcesPerReplacement {
		last := min(first+documentstore.MaximumOutboundAnchorSourcesPerReplacement, len(sets))
		if c.replaceOutboundAnchors(ctx, deliveries, sets[first:last], reservation) {
			return true
		}
	}

	return false
}
