package yagocrawlcontract

import (
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func TestIngestBatchRoundTrip(t *testing.T) {
	familyFriendly := false
	batch := IngestBatch{
		SourceURL:        "https://example.org/a",
		Provenance:       []byte("admin"),
		ProfileHandle:    "abcdef012345",
		ObservationID:    "5e93886c-58f8-47d6-b38d-52e5d96b82e3",
		ObservedAt:       time.Date(2026, 7, 2, 10, 0, 2, 0, time.UTC),
		SourceModifiedAt: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
		Document: DocumentIngest{
			CanonicalURL:     "https://example.org/a",
			NormalizedURL:    "https://example.org/a",
			Title:            "Title",
			Headings:         []string{"Heading"},
			ExtractedText:    "body text",
			Language:         "en",
			ContentType:      "text/html",
			FetchStatus:      "fetched",
			FetchedAt:        time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC),
			IndexedAt:        time.Date(2026, 7, 2, 10, 0, 1, 0, time.UTC),
			PublishedAt:      time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC),
			ModifiedAt:       time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
			FirstSeenAt:      time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
			ContentChangedAt: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
			DateConfidence:   1,
			DateSource:       "json-ld",
			ContentHash:      "abc",
			Outlinks:         []string{"https://example.org/b"},
			Inlinks: []AnchorText{{
				URL: "https://example.org/", Text: "anchor", UserGenerated: true,
			}},
			OutboundAnchors: []OutboundAnchor{{
				TargetURL: "https://example.org/b", Text: "next", Sponsored: true,
			}},
			OutboundAnchorEvidenceKnown: true,
			SafetyLabels: SafetyLabels{
				RatingValues:   []string{"adult"},
				FamilyFriendly: &familyFriendly,
			},
			Images: []ImageMetadata{{
				URL:     "https://example.org/image.png",
				AltText: "Example image",
			}},
			Metadata: map[string]string{"profile": "abcdef012345"},
		},
		Postings: []yagomodel.RWIPosting{
			{
				WordHash:   yagomodel.Hash("wordhash0123"),
				Properties: map[string]string{"u": "urlhash01234"},
			},
		},
		Metadata: []yagomodel.URIMetadataRow{
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

func TestIngestBatchRemovedRoundTrip(t *testing.T) {
	batch := IngestBatch{
		SourceURL:     "https://example.org/gone",
		Provenance:    []byte("admin"),
		ProfileHandle: "abcdef012345",
		ObservationID: "a136c9a7-b5c9-4bf4-ae3a-cb4ee62d1f16",
		ObservedAt:    time.Date(2026, 7, 2, 10, 0, 2, 0, time.UTC),
		Removed:       true,
	}

	data, err := MarshalIngestBatch(batch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalIngestBatch(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Removed {
		t.Fatalf("Removed did not survive round-trip: %#v", got)
	}
	if !reflect.DeepEqual(batch, got) {
		t.Errorf("round-trip mismatch:\nwant %#v\ngot  %#v", batch, got)
	}
}
