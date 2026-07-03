package yacycrawlcontract

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

const ExtractedTextStreamName = "YACY_CRAWL_EXTRACTED_TEXT"

type ExtractedTextStreamSpec struct {
	Subject string
	MaxMsgs int64
}

func EnsureExtractedTextStream(ctx context.Context, js jetstream.JetStream, spec ExtractedTextStreamSpec) error {
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      ExtractedTextStreamName,
		Subjects:  []string{spec.Subject},
		Retention: jetstream.WorkQueuePolicy,
		MaxMsgs:   spec.MaxMsgs,
		Discard:   jetstream.DiscardNew,
	}); err != nil {
		return fmt.Errorf("ensure extracted text stream: %w", err)
	}
	return nil
}
