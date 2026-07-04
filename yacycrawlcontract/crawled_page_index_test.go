package yacycrawlcontract

import (
	"reflect"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestCrawledPageIndexRoundTrip(t *testing.T) {
	index := CrawledPageIndex{
		SourceURL:     "https://example.org/a",
		Provenance:    []byte("admin"),
		ProfileHandle: "abcdef012345",
		Postings: []yacymodel.RWIPosting{
			{
				WordHash:   yacymodel.Hash("wordhash0123"),
				Properties: map[string]string{"u": "urlhash01234"},
			},
		},
		Metadata: []yacymodel.URIMetadataRow{
			{Properties: map[string]string{"u": "urlhash01234", "t": "Title"}},
		},
	}

	data, err := MarshalCrawledPageIndex(index)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalCrawledPageIndex(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(index, got) {
		t.Errorf("round-trip mismatch:\nwant %#v\ngot  %#v", index, got)
	}
}
