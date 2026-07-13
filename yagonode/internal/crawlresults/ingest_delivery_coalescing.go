package crawlresults

import (
	"context"
)

func coalesceIngestDeliveries(deliveries []IngestDelivery) []IngestDelivery {
	positions := make(map[string]int, len(deliveries))
	coalesced := make([]IngestDelivery, 0, len(deliveries))
	settlements := make([][]IngestDelivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		position, found := positions[delivery.Batch.SourceURL]
		if !found {
			positions[delivery.Batch.SourceURL] = len(coalesced)
			coalesced = append(coalesced, delivery)
			settlements = append(settlements, []IngestDelivery{delivery})
			continue
		}
		if ingestDeliverySupersedes(delivery, coalesced[position]) {
			coalesced[position] = delivery
		}
		settlements[position] = append(settlements[position], delivery)
	}
	for position := range coalesced {
		if len(settlements[position]) == 1 {
			continue
		}
		group := append([]IngestDelivery(nil), settlements[position]...)
		coalesced[position].Ack = func(ctx context.Context) error {
			return settleIngestDeliveries(ctx, group, true)
		}
		coalesced[position].Nak = func(ctx context.Context) error {
			return settleIngestDeliveries(ctx, group, false)
		}
	}

	return coalesced
}

func ingestDeliverySupersedes(candidate, current IngestDelivery) bool {
	candidateRecord, candidateErr := observationRecord(candidate.Batch)
	currentRecord, currentErr := observationRecord(current.Batch)
	if candidateErr == nil && currentErr != nil {
		return true
	}
	if candidateErr != nil && currentErr == nil {
		return false
	}
	if candidateErr != nil {
		return candidate.Batch.ProfileHandle > current.Batch.ProfileHandle
	}

	return compareObservations(candidateRecord, currentRecord) > 0
}

func settleIngestDeliveries(ctx context.Context, deliveries []IngestDelivery, ack bool) error {
	var first error
	for _, delivery := range deliveries {
		settle := delivery.Nak
		if ack {
			settle = delivery.Ack
		}
		if err := settle(ctx); err != nil && first == nil {
			first = err
		}
	}

	return first
}
