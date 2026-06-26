package yacycrawler

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const (
	OrdersStreamName = yacycrawlcontract.OrdersStreamName
	IngestStreamName = yacycrawlcontract.IngestStreamName
)

type StreamSpec = yacycrawlcontract.StreamSpec

func EnsureStreams(ctx context.Context, js jetstream.JetStream, spec StreamSpec) error {
	if err := yacycrawlcontract.EnsureStreams(ctx, js, spec); err != nil {
		return fmt.Errorf("ensure streams: %w", err)
	}
	return nil
}
