package crawledpageindex_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawledpageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type fakeCrawledPageIndexPublisher struct {
	published chan crawledpageindex.CrawledPageIndex
}

func newFakeCrawledPageIndexPublisher() *fakeCrawledPageIndexPublisher {
	return &fakeCrawledPageIndexPublisher{
		published: make(chan crawledpageindex.CrawledPageIndex, 1),
	}
}

func (p *fakeCrawledPageIndexPublisher) Publish(
	ctx context.Context,
	index crawledpageindex.CrawledPageIndex,
) error {
	select {
	case p.published <- index:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("publish: %w", ctx.Err())
	}
}

func TestCrawledPageIndexEmitterAssemblesEnvelope(t *testing.T) {
	queue := newFakeCrawledPageIndexPublisher()
	emitter := crawledpageindex.NewCrawledPageIndexEmitter(queue)

	postings := []yacymodel.RWIPosting{{WordHash: yacymodel.WordHash("kangaroo")}}
	metadata := yacymodel.URIMetadataRow{Properties: map[string]string{"u": "x"}}
	envelope := crawledpageindex.Envelope{
		SourceURL:     "http://example.com/",
		Provenance:    []byte("peer"),
		ProfileHandle: "handle",
	}

	if err := emitter.Emit(context.Background(), postings, metadata, envelope); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	index := <-queue.published
	if index.SourceURL != envelope.SourceURL {
		t.Errorf("source url = %q", index.SourceURL)
	}
	if index.ProfileHandle != envelope.ProfileHandle {
		t.Errorf("handle = %q", index.ProfileHandle)
	}
	if len(index.Postings) != 1 || index.Postings[0].WordHash != yacymodel.WordHash("kangaroo") {
		t.Errorf("postings = %v", index.Postings)
	}
	if len(index.Metadata) != 1 {
		t.Errorf("metadata rows = %d, want 1", len(index.Metadata))
	}
}
