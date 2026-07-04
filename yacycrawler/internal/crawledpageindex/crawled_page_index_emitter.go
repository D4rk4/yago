package crawledpageindex

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type Envelope struct {
	SourceURL     string
	Provenance    []byte
	ProfileHandle string
}

type CrawledPageIndexEmitter interface {
	Emit(
		ctx context.Context,
		postings []yacymodel.RWIPosting,
		metadata yacymodel.URIMetadataRow,
		envelope Envelope,
	) error
}

type CrawledPageIndexPublisher interface {
	Publish(ctx context.Context, index CrawledPageIndex) error
}

type crawledPageIndexEmitter struct {
	queue CrawledPageIndexPublisher
}

func NewCrawledPageIndexEmitter(
	queue CrawledPageIndexPublisher,
) CrawledPageIndexEmitter {
	return &crawledPageIndexEmitter{queue: queue}
}

func (e *crawledPageIndexEmitter) Emit(
	ctx context.Context,
	postings []yacymodel.RWIPosting,
	metadata yacymodel.URIMetadataRow,
	envelope Envelope,
) error {
	index := CrawledPageIndex{
		SourceURL:     envelope.SourceURL,
		Provenance:    envelope.Provenance,
		ProfileHandle: envelope.ProfileHandle,
		Postings:      postings,
		Metadata:      []yacymodel.URIMetadataRow{metadata},
	}
	if err := e.queue.Publish(ctx, index); err != nil {
		return fmt.Errorf("publish crawled page index %s: %w", envelope.SourceURL, err)
	}
	return nil
}
