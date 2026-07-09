package pagepublication

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestSegmentEmitsMetadataFirstThenBoundedPostings(t *testing.T) {
	postings := make([]yacymodel.RWIPosting, yacycrawlcontract.PostingsPerMessageLimit*2+1)
	for i := range postings {
		postings[i] = yacymodel.RWIPosting{WordHash: yacymodel.WordHash("w")}
	}
	index := yacycrawlcontract.CrawledPageIndex{
		CanonicalURL: "https://example.org/a",
		Metadata: []yacymodel.URIMetadataRow{
			{Properties: map[string]string{"u": "urlhash01234"}},
		},
		Postings: postings,
	}

	messages := segmentCrawledPageIndex(index)

	if len(messages) != 4 {
		t.Fatalf("segments = %d, want 4 (1 metadata + 3 posting batches)", len(messages))
	}
	if len(messages[0].Metadata) != 1 || len(messages[0].Postings) != 0 {
		t.Fatalf("first message = %+v, want metadata only", messages[0])
	}
	for i, message := range messages[1:] {
		if len(message.Metadata) != 0 {
			t.Fatalf("posting message %d carries metadata", i)
		}
		if len(message.Postings) > yacycrawlcontract.PostingsPerMessageLimit {
			t.Fatalf("posting message %d has %d postings, over limit %d",
				i, len(message.Postings), yacycrawlcontract.PostingsPerMessageLimit)
		}
		if message.CanonicalURL != index.CanonicalURL {
			t.Fatalf("posting message %d url = %q, want %q",
				i, message.CanonicalURL, index.CanonicalURL)
		}
	}
}

func TestSegmentWithoutPostingsEmitsMetadataOnly(t *testing.T) {
	index := yacycrawlcontract.CrawledPageIndex{
		CanonicalURL: "https://example.org/a",
		Metadata: []yacymodel.URIMetadataRow{
			{Properties: map[string]string{"u": "urlhash01234"}},
		},
	}

	messages := segmentCrawledPageIndex(index)

	if len(messages) != 1 || len(messages[0].Metadata) != 1 {
		t.Fatalf("segments = %+v, want a single metadata message", messages)
	}
}
