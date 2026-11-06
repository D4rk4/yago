package crawlresults

import (
	"context"
	"log/slog"
)

const (
	msgNotModifiedRecorded = "not-modified observation recorded"
	msgNotModifiedRefused  = "not-modified observation refused"
)

func (c *IngestConsumer) absorbNotModifiedDeliveries(
	ctx context.Context,
	deliveries []IngestDelivery,
) []IngestDelivery {
	remaining := make([]IngestDelivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.Batch.NotModified == nil {
			remaining = append(remaining, delivery)

			continue
		}
		c.absorbNotModified(ctx, delivery)
	}

	return remaining
}

func (c *IngestConsumer) absorbNotModified(ctx context.Context, delivery IngestDelivery) {
	if !c.passesGates(ctx, delivery) {
		return
	}
	sourceURL := delivery.Batch.SourceURL
	recorded, err := c.conditional.RecordNotModifiedObservation(ctx, delivery.Batch)
	if err != nil {
		c.redeliver(ctx, delivery, sourceURL, "not-modified observation", err)

		return
	}
	settle := delivery.Ack
	if recorded {
		slog.DebugContext(ctx, msgNotModifiedRecorded, slog.String("sourceUrl", sourceURL))
		if delivery.AckRecordedNotModified != nil {
			settle = delivery.AckRecordedNotModified
		}
	} else {
		slog.WarnContext(ctx, msgNotModifiedRefused, slog.String("sourceUrl", sourceURL))
	}
	_ = settle(ctx)
}
