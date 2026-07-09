package pagepublication

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestSegmentEmitsMetadataFirstThenBoundedPostings(t *testing.T) {
	postings := make([]yacymodel.RWIPosting, yacycrawlcontract.PostingsPerSegmentLimit*2+1)
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

	segments := segmentCrawledPageIndex(index)

	if len(segments) != 4 {
		t.Fatalf("segments = %d, want 4 (1 metadata + 3 posting batches)", len(segments))
	}
	if len(segments[0].Metadata) != 1 || len(segments[0].Postings) != 0 {
		t.Fatalf("first segment = %+v, want metadata only", segments[0])
	}
	for i, segment := range segments[1:] {
		if len(segment.Metadata) != 0 {
			t.Fatalf("posting segment %d carries metadata", i)
		}
		if len(segment.Postings) > yacycrawlcontract.PostingsPerSegmentLimit {
			t.Fatalf("posting segment %d has %d postings, over limit %d",
				i, len(segment.Postings), yacycrawlcontract.PostingsPerSegmentLimit)
		}
		if segment.CanonicalURL != index.CanonicalURL {
			t.Fatalf("posting segment %d url = %q, want %q",
				i, segment.CanonicalURL, index.CanonicalURL)
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

	segments := segmentCrawledPageIndex(index)

	if len(segments) != 1 || len(segments[0].Metadata) != 1 {
		t.Fatalf("segments = %+v, want a single metadata segment", segments)
	}
}
