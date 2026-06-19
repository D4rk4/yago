package yacycrawlcontract

import (
	"reflect"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestIngestBatchRoundTrip(t *testing.T) {
	batch := IngestBatch{
		SourceURL:     "https://example.org/a",
		Provenance:    []byte("admin"),
		ProfileHandle: "abcdef012345",
		Postings: []yacymodel.RWIEntry{
			{
				WordHash:   yacymodel.Hash("wordhash0123"),
				Properties: map[string]string{"u": "urlhash01234"},
			},
		},
		Metadata: []yacymodel.URIMetadataRow{
			{Properties: map[string]string{"u": "urlhash01234", "t": "Title"}},
		},
	}

	data, err := MarshalIngestBatch(batch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalIngestBatch(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(batch, got) {
		t.Errorf("round-trip mismatch:\nwant %#v\ngot  %#v", batch, got)
	}
}
