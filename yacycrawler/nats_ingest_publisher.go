package yacycrawler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const ingestStreamFullErrorCode jetstream.ErrorCode = 10077

const DefaultIngestRetryWait = 100 * time.Millisecond

type NATSIngestPublisher struct {
	js        jetstream.JetStream
	subject   string
	retryWait time.Duration
}

func NewNATSIngestPublisher(js jetstream.JetStream, subject string) *NATSIngestPublisher {
	return &NATSIngestPublisher{js: js, subject: subject, retryWait: DefaultIngestRetryWait}
}

func (p *NATSIngestPublisher) Publish(ctx context.Context, batch IngestBatch) error {
	data, err := yacycrawlcontract.MarshalIngestBatch(batch)
	if err != nil {
		return fmt.Errorf("encode ingest batch %s: %w", batch.SourceURL, err)
	}
	for {
		if _, err := p.js.Publish(ctx, p.subject, data); err == nil {
			return nil
		} else if !ingestStreamFull(err) {
			return fmt.Errorf("publish ingest batch %s: %w", batch.SourceURL, err)
		}
		select {
		case <-time.After(p.retryWait):
		case <-ctx.Done():
			return fmt.Errorf("publish ingest batch %s: %w", batch.SourceURL, ctx.Err())
		}
	}
}

func ingestStreamFull(err error) bool {
	apiErr, ok := errors.AsType[*jetstream.APIError](err)
	return ok && apiErr.ErrorCode == ingestStreamFullErrorCode
}
