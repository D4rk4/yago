package crawledpageindex

import (
	"errors"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var ErrCrawledPageIndexOversized = errors.New("crawled page index exceeds the transport size limit")

const crawledPageIndexOversizedErrorCode jetstream.ErrorCode = 10054

func crawledPageIndexOversized(err error) bool {
	if errors.Is(err, nats.ErrMaxPayload) {
		return true
	}
	apiErr, ok := errors.AsType[*jetstream.APIError](err)
	return ok && apiErr.ErrorCode == crawledPageIndexOversizedErrorCode
}
