package pagepublication

import (
	"context"
	"errors"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

const streamStoreFailedCode jetstream.ErrorCode = 10077

func classifyPublishError(err error) error {
	if err == nil {
		return nil
	}
	if isTransient(err) {
		return crawlcapability.TransientPublicationError{Err: err}
	}
	return err
}

func isTransient(err error) bool {
	if errors.Is(err, jetstream.ErrNoStreamResponse) ||
		errors.Is(err, nats.ErrTimeout) ||
		errors.Is(err, nats.ErrNoResponders) ||
		errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var apiErr *jetstream.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode == streamStoreFailedCode
	}
	return false
}
