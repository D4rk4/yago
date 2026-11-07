package crawlresults

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const msgResponseValidatorsRecordFailed = "conditional recrawl validators record failed"

type ConditionalRecrawlRecorder interface {
	RecordResponseValidators(context.Context, yagocrawlcontract.IngestBatch) error
	RecordNotModifiedObservation(context.Context, yagocrawlcontract.IngestBatch) (bool, error)
}

type unconditionalRecrawl struct{}

func (unconditionalRecrawl) RecordResponseValidators(
	context.Context,
	yagocrawlcontract.IngestBatch,
) error {
	return nil
}

func (unconditionalRecrawl) RecordNotModifiedObservation(
	context.Context,
	yagocrawlcontract.IngestBatch,
) (bool, error) {
	return false, nil
}

func (c *IngestConsumer) RecordConditionalRecrawl(recorder ConditionalRecrawlRecorder) {
	c.conditional = recorder
}

func (c *IngestConsumer) recordResponseValidators(
	ctx context.Context,
	deliveries []IngestDelivery,
) {
	for _, delivery := range deliveries {
		if err := c.conditional.RecordResponseValidators(ctx, delivery.Batch); err != nil {
			slog.WarnContext(ctx, msgResponseValidatorsRecordFailed,
				slog.String("sourceUrl", delivery.Batch.SourceURL), slog.Any("error", err))
		}
	}
}
