package crawlresults

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	msgIngestObservationDuplicate  = "ingest observation already committed"
	msgIngestObservationSuperseded = "ingest observation superseded"
)

type acceptAllObservationHistory struct{}

func (acceptAllObservationHistory) Begin(
	context.Context,
	[]yagocrawlcontract.IngestBatch,
) ([]observationDisposition, error) {
	return nil, nil
}

func (acceptAllObservationHistory) Complete(
	context.Context,
	[]yagocrawlcontract.IngestBatch,
) error {
	return nil
}

func (c *IngestConsumer) OrderObservations(history *URLObservationHistory) {
	if history != nil {
		c.observations = history
	}
}

func (c *IngestConsumer) beginObservations(
	ctx context.Context,
	deliveries []IngestDelivery,
) []IngestDelivery {
	if len(deliveries) == 0 {
		return deliveries
	}
	if c.observations == nil {
		return deliveries
	}
	batches := ingestBatches(deliveries)
	dispositions, err := c.observations.Begin(ctx, batches)
	if err != nil {
		c.redeliverGroup(ctx, deliveries, "observation ordering", err)

		return nil
	}
	if len(dispositions) == 0 {
		return deliveries
	}
	current := deliveries[:0]
	for index, delivery := range deliveries {
		switch dispositions[index] {
		case observationApply:
			current = append(current, delivery)
		case observationDuplicate:
			c.acknowledgeOrderedObservation(ctx, delivery, observationDuplicate)
		case observationSuperseded:
			c.acknowledgeOrderedObservation(ctx, delivery, observationSuperseded)
		}
	}

	return current
}

func (c *IngestConsumer) completeObservations(
	ctx context.Context,
	deliveries []IngestDelivery,
) bool {
	if len(deliveries) == 0 {
		return true
	}
	if c.observations == nil {
		return true
	}
	if err := c.observations.Complete(ctx, ingestBatches(deliveries)); err != nil {
		c.redeliverGroup(ctx, deliveries, "observation completion", err)

		return false
	}

	return true
}

func ingestBatches(deliveries []IngestDelivery) []yagocrawlcontract.IngestBatch {
	batches := make([]yagocrawlcontract.IngestBatch, 0, len(deliveries))
	for _, delivery := range deliveries {
		batches = append(batches, delivery.Batch)
	}

	return batches
}

func (c *IngestConsumer) acknowledgeOrderedObservation(
	ctx context.Context,
	delivery IngestDelivery,
	disposition observationDisposition,
) {
	if err := delivery.Ack(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestAckFailed,
			slog.String("sourceUrl", delivery.Batch.SourceURL), slog.Any("error", err))

		return
	}
	if disposition == observationDuplicate {
		slog.DebugContext(ctx, msgIngestObservationDuplicate,
			slog.String("sourceUrl", delivery.Batch.SourceURL))

		return
	}
	slog.DebugContext(ctx, msgIngestObservationSuperseded,
		slog.String("sourceUrl", delivery.Batch.SourceURL))
}
