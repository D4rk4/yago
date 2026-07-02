package yacycrawlcontract

import (
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
)

func TestIngestBatchRoundTrip(t *testing.T) {
	batch := IngestBatch{
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

func TestUnmarshalIngestBatchRejectsInvalidJSON(t *testing.T) {
	if _, err := UnmarshalIngestBatch([]byte("{")); err == nil {
		t.Fatal("invalid JSON should fail")
	}
}
