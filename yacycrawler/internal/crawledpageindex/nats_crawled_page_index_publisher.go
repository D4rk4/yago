package crawledpageindex

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

type CrawledPageIndex = yacycrawlcontract.CrawledPageIndex

const crawledPageIndexStreamFullErrorCode jetstream.ErrorCode = 10077

const DefaultCrawledPageIndexRetryWait = 100 * time.Millisecond

type NATSCrawledPageIndexPublisher struct {
	js        jetstream.JetStream
	subject   string
	retryWait time.Duration
}

func NewNATSCrawledPageIndexPublisher(
	js jetstream.JetStream,
	subject string,
) *NATSCrawledPageIndexPublisher {
	return &NATSCrawledPageIndexPublisher{
		js:        js,
		subject:   subject,
		retryWait: DefaultCrawledPageIndexRetryWait,
	}
}

func (p *NATSCrawledPageIndexPublisher) Publish(ctx context.Context, index CrawledPageIndex) error {
	data, err := yacycrawlcontract.MarshalCrawledPageIndex(index)
	if err != nil {
		return fmt.Errorf("encode crawled page index %s: %w", index.SourceURL, err)
	}
	for {
		if _, err := p.js.Publish(ctx, p.subject, data); err == nil {
			return nil
		} else if !crawledPageIndexStreamFull(err) {
			return fmt.Errorf("publish crawled page index %s: %w", index.SourceURL, err)
		}
		select {
		case <-time.After(p.retryWait):
		case <-ctx.Done():
			return fmt.Errorf("publish crawled page index %s: %w", index.SourceURL, ctx.Err())
		}
	}
}

func crawledPageIndexStreamFull(err error) bool {
	apiErr, ok := errors.AsType[*jetstream.APIError](err)
	return ok && apiErr.ErrorCode == crawledPageIndexStreamFullErrorCode
}
