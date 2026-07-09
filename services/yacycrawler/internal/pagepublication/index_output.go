package pagepublication

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageindex"
)

const indexOutputName = "index"

type IndexOutput struct {
	publisher jetstream.JetStream
	subject   string
}

func NewIndexOutput(publisher jetstream.JetStream, subject string) IndexOutput {
	return IndexOutput{publisher: publisher, subject: subject}
}

func (IndexOutput) Name() string {
	return indexOutputName
}

func (o IndexOutput) Publish(ctx context.Context, page crawlcapability.ExtractedPage) error {
	index, err := pageindex.Build(page)
	if err != nil {
		return fmt.Errorf("build page index: %w", err)
	}
	for _, message := range segmentCrawledPageIndex(index) {
		payload, err := yacycrawlcontract.MarshalCrawledPageIndexMessage(message)
		if err != nil {
			return fmt.Errorf("marshal page index message: %w", err)
		}
		if _, err := o.publisher.Publish(ctx, o.subject, payload); err != nil {
			return classifyPublishError(err)
		}
	}
	return nil
}
