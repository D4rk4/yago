package yacycrawlcontract

import (
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

func TestIngestBatchRoundTrip(t *testing.T) {
	batch := IngestBatch{
		SourceURL:     "https://example.org/a",
		Provenance:    []byte("admin"),
		ProfileHandle: "abcdef012345",
		Document: DocumentIngest{
			CanonicalURL:  "https://example.org/a",
			NormalizedURL: "https://example.org/a",
			Title:         "Title",
			Headings:      []string{"Heading"},
			ExtractedText: "body text",
			Language:      "en",
			ContentType:   "text/html",
			FetchStatus:   "fetched",
			FetchedAt:     time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC),
			IndexedAt:     time.Date(2026, 7, 2, 10, 0, 1, 0, time.UTC),
			ContentHash:   "abc",
			Outlinks:      []string{"https://example.org/b"},
			Inlinks:       []AnchorText{{URL: "https://example.org/", Text: "anchor"}},
			Metadata:      map[string]string{"profile": "abcdef012345"},
		},
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
