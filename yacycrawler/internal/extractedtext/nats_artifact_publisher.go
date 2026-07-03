package extractedtext

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const extractedTextStreamFullErrorCode jetstream.ErrorCode = 10077

const DefaultExtractedTextRetryWait = 100 * time.Millisecond

type NATSArtifactPublisher struct {
	js        jetstream.JetStream
	subject   string
	retryWait time.Duration
}

func NewNATSArtifactPublisher(js jetstream.JetStream, subject string) *NATSArtifactPublisher {
	return &NATSArtifactPublisher{js: js, subject: subject, retryWait: DefaultExtractedTextRetryWait}
}

func (p *NATSArtifactPublisher) Publish(ctx context.Context, text yacycrawlcontract.ExtractedText) error {
	data, err := yacycrawlcontract.MarshalExtractedText(text)
	if err != nil {
		return fmt.Errorf("encode extracted text %s: %w", text.CanonicalURL, err)
	}
	for {
		if _, err := p.js.Publish(ctx, p.subject, data); err == nil {
			return nil
		} else if !extractedTextStreamFull(err) {
			return fmt.Errorf("publish extracted text %s: %w", text.CanonicalURL, err)
		}
		select {
		case <-time.After(p.retryWait):
		case <-ctx.Done():
			return fmt.Errorf("publish extracted text %s: %w", text.CanonicalURL, ctx.Err())
		}
	}
}

func extractedTextStreamFull(err error) bool {
	apiErr, ok := errors.AsType[*jetstream.APIError](err)
	return ok && apiErr.ErrorCode == extractedTextStreamFullErrorCode
}
