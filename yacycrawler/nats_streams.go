package yacycrawler

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	OrdersStreamName = "YACY_CRAWL_ORDERS"
	IngestStreamName = "YACY_CRAWL_INGEST"
)

type StreamSpec struct {
	OrdersSubject string
	IngestSubject string
	IngestMaxMsgs int64
}

func EnsureStreams(ctx context.Context, js jetstream.JetStream, spec StreamSpec) error {
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      OrdersStreamName,
		Subjects:  []string{spec.OrdersSubject},
		Retention: jetstream.WorkQueuePolicy,
	}); err != nil {
		return fmt.Errorf("ensure orders stream: %w", err)
	}
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      IngestStreamName,
		Subjects:  []string{spec.IngestSubject},
		Retention: jetstream.WorkQueuePolicy,
		MaxMsgs:   spec.IngestMaxMsgs,
		Discard:   jetstream.DiscardNew,
	}); err != nil {
		return fmt.Errorf("ensure ingest stream: %w", err)
	}
	return nil
}
