package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

var ErrNotModifiedUnrecorded = errors.New("node did not record the not-modified observation")

func NewNotModifiedBatch(
	observation yagocrawlcontract.NotModifiedObservation,
	envelope Envelope,
) IngestBatch {
	return IngestBatch{
		SourceURL:          envelope.SourceURL,
		Provenance:         envelope.Provenance,
		ProfileHandle:      envelope.ProfileHandle,
		ObservationID:      ingestObservationID(envelope.ObservationID),
		ObservedAt:         ingestObservationTime(envelope.ObservedAt, time.Time{}),
		ResponseValidators: usableResponseValidators(envelope.ResponseValidators),
		NotModified:        &observation,
	}
}

func (e *batchEmitter) EmitNotModified(
	ctx context.Context,
	observation yagocrawlcontract.NotModifiedObservation,
	envelope Envelope,
) error {
	if err := e.queue.Publish(ctx, NewNotModifiedBatch(observation, envelope)); err != nil {
		return fmt.Errorf("publish not-modified batch %s: %w", envelope.SourceURL, err)
	}

	return nil
}

func usableResponseValidators(
	validators *yagocrawlcontract.HTTPValidators,
) *yagocrawlcontract.HTTPValidators {
	if validators == nil || validators.Validate() != nil {
		return nil
	}

	return validators
}
