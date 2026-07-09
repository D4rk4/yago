package yacycrawlcontract

import (
	"reflect"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestCrawledPageIndexSegmentRoundTrip(t *testing.T) {
	segment := CrawledPageIndexSegment{
		CanonicalURL: "https://example.org/a",
		Postings: []yacymodel.RWIPosting{
			{
				WordHash:   yacymodel.Hash("wordhash0123"),
				Properties: map[string]string{"u": "urlhash01234"},
			},
		},
	}

	data, err := MarshalCrawledPageIndexSegment(segment)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalCrawledPageIndexSegment(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(segment, got) {
		t.Errorf("round-trip mismatch:\nwant %#v\ngot  %#v", segment, got)
	}
}
