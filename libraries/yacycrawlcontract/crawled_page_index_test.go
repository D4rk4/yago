package yacycrawlcontract

import (
	"reflect"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestCrawledPageIndexMessageRoundTrip(t *testing.T) {
	message := CrawledPageIndexMessage{
		CanonicalURL: "https://example.org/a",
		Postings: []yacymodel.RWIPosting{
			{
				WordHash:   yacymodel.Hash("wordhash0123"),
				Properties: map[string]string{"u": "urlhash01234"},
			},
		},
	}

	data, err := MarshalCrawledPageIndexMessage(message)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalCrawledPageIndexMessage(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(message, got) {
		t.Errorf("round-trip mismatch:\nwant %#v\ngot  %#v", message, got)
	}
}
