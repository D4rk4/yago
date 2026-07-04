package crawledpage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const crawledPageStreamFullErrorCode jetstream.ErrorCode = 10077

const DefaultCrawledPageRetryWait = 100 * time.Millisecond

type NATSCrawledPagePublisher struct {
	js        jetstream.JetStream
	subject   string
	retryWait time.Duration
}

func NewNATSCrawledPagePublisher(js jetstream.JetStream, subject string) *NATSCrawledPagePublisher {
	return &NATSCrawledPagePublisher{
		js:        js,
		subject:   subject,
		retryWait: DefaultCrawledPageRetryWait,
	}
}

func (p *NATSCrawledPagePublisher) Publish(
	ctx context.Context,
	text yacycrawlcontract.CrawledPage,
) error {
	data, err := yacycrawlcontract.MarshalCrawledPage(text)
	if err != nil {
		return fmt.Errorf("encode crawled page %s: %w", text.CanonicalURL, err)
	}
	for {
		if _, err := p.js.Publish(ctx, p.subject, data); err == nil {
			return nil
		} else if crawledPageOversized(err) {
			return fmt.Errorf(
				"publish crawled page %s: %w",
				text.CanonicalURL,
				ErrCrawledPageOversized,
			)
		} else if !crawledPageStreamFull(err) {
			return fmt.Errorf("publish crawled page %s: %w", text.CanonicalURL, err)
		}
		select {
		case <-time.After(p.retryWait):
		case <-ctx.Done():
			return fmt.Errorf("publish crawled page %s: %w", text.CanonicalURL, ctx.Err())
		}
	}
}

func crawledPageStreamFull(err error) bool {
	apiErr, ok := errors.AsType[*jetstream.APIError](err)
	return ok && apiErr.ErrorCode == crawledPageStreamFullErrorCode
}
