package yacycrawlcontract

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	OrdersStreamName           = "YACY_CRAWL_ORDERS"
	CrawledPageIndexStreamName = "YACY_CRAWL_PAGE_INDEX"
	CrawledPageStreamName      = "YACY_CRAWL_PAGES"
)

type OrdersStreamSpec struct {
	Subject string
}

type CrawledPageIndexStreamSpec struct {
	Subject string
	MaxMsgs int64
}

type CrawledPageStreamSpec struct {
	Subject string
	MaxMsgs int64
}

func EnsureOrdersStream(ctx context.Context, js jetstream.JetStream, spec OrdersStreamSpec) error {
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      OrdersStreamName,
		Subjects:  []string{spec.Subject},
		Retention: jetstream.WorkQueuePolicy,
	}); err != nil {
		return fmt.Errorf("ensure orders stream: %w", err)
	}
	return nil
}

func EnsureCrawledPageIndexStream(
	ctx context.Context,
	js jetstream.JetStream,
	spec CrawledPageIndexStreamSpec,
) error {
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      CrawledPageIndexStreamName,
		Subjects:  []string{spec.Subject},
		Retention: jetstream.WorkQueuePolicy,
		MaxMsgs:   spec.MaxMsgs,
		Discard:   jetstream.DiscardNew,
	}); err != nil {
		return fmt.Errorf("ensure crawled page index stream: %w", err)
	}
	return nil
}

func EnsureCrawledPageStream(
	ctx context.Context,
	js jetstream.JetStream,
	spec CrawledPageStreamSpec,
) error {
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      CrawledPageStreamName,
		Subjects:  []string{spec.Subject},
		Retention: jetstream.WorkQueuePolicy,
		MaxMsgs:   spec.MaxMsgs,
		Discard:   jetstream.DiscardNew,
	}); err != nil {
		return fmt.Errorf("ensure crawled page stream: %w", err)
	}
	return nil
}
