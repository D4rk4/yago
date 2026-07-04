package crawledpageindex_test

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/boundedqueue"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawledpageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestCrawledPageIndexEmitterAssemblesEnvelope(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawledpageindex.CrawledPageIndex](1)
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

	index := <-queue.Receive()
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
