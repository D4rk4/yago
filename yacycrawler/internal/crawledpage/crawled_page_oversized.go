package crawledpage

import (
	"errors"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var ErrCrawledPageOversized = errors.New("crawled page exceeds the transport size limit")

const crawledPageOversizedErrorCode jetstream.ErrorCode = 10054

func crawledPageOversized(err error) bool {
	if errors.Is(err, nats.ErrMaxPayload) {
		return true
	}
	apiErr, ok := errors.AsType[*jetstream.APIError](err)
	return ok && apiErr.ErrorCode == crawledPageOversizedErrorCode
}
