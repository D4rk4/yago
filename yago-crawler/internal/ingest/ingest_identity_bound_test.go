package ingest

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

// TestPrepareIngestMessageNamesTheOverlongIdentity pins every identity the
// guard checks, not just the source URL. All three travel to the node as the
// document's primary keys, and the node's URL-identity store refuses anything
// past MaximumCrawlURLBytes — so a canonical or normalized URL that slipped
// through here would be rejected at the far end after the whole batch had been
// marshalled and shipped. Asserting the named identity is what makes a dropped
// or reordered check visible: a test that only asked "some error" would keep
// passing with two of the three checks deleted.
func TestPrepareIngestMessageNamesTheOverlongIdentity(t *testing.T) {
	overlong := "https://example.org/" + strings.Repeat(
		"x",
		yagocrawlcontract.MaximumCrawlURLBytes,
	)
	tests := []struct {
		name  string
		batch IngestBatch
		want  string
	}{
		{
			name:  "source URL",
			batch: IngestBatch{SourceURL: overlong},
			want:  "source URL",
		},
		{
			name: "canonical URL",
			batch: IngestBatch{Document: yagocrawlcontract.DocumentIngest{
				CanonicalURL: overlong,
			}},
			want: "canonical URL",
		},
		{
			name: "normalized URL",
			batch: IngestBatch{Document: yagocrawlcontract.DocumentIngest{
				NormalizedURL: overlong,
			}},
			want: "normalized URL",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := prepareIngestMessage(test.batch)
			if !errors.Is(err, errIngestIdentityTooLarge) {
				t.Fatalf("error = %v, want identity limit", err)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want it to name %q", err, test.want)
			}
		})
	}
}

// TestPrepareIngestMessageAcceptsIdentityAtTheLimit is the accepting half of
// the identity bound. MaximumCrawlURLBytes is the largest identity the contract
// carries, so a URL of exactly that length is legal and must survive the guard
// and the bounding pass byte-for-byte: an off-by-one here silently drops every
// page whose URL sits on the limit, and truncating one would hand the node a
// different document than was crawled.
func TestPrepareIngestMessageAcceptsIdentityAtTheLimit(t *testing.T) {
	identity := maximumLengthIngestURL()
	if len(identity) != yagocrawlcontract.MaximumCrawlURLBytes {
		t.Fatalf("fixture length = %d", len(identity))
	}
	data, err := prepareIngestMessage(IngestBatch{
		SourceURL: identity,
		Document: yagocrawlcontract.DocumentIngest{
			CanonicalURL:  identity,
			NormalizedURL: identity,
		},
	})
	if err != nil {
		t.Fatalf("prepare ingest: %v", err)
	}
	decoded, err := yagocrawlcontract.UnmarshalIngestBatch(data)
	if err != nil {
		t.Fatalf("decode prepared ingest: %v", err)
	}
	if decoded.SourceURL != identity ||
		decoded.Document.CanonicalURL != identity ||
		decoded.Document.NormalizedURL != identity {
		t.Fatalf("identity lengths = %d/%d/%d, want %d unchanged",
			len(decoded.SourceURL),
			len(decoded.Document.CanonicalURL),
			len(decoded.Document.NormalizedURL),
			len(identity))
	}
}

// TestBoundedIngestBatchLeavesAnAlreadyBoundedBatchUnchanged is the accepting
// side of every bound in the pass, in one place. Each field sits exactly on its
// limit, which is legal: the bounding pass is a repair, and a repair applied to
// already-correct input must return it unchanged. An inclusive comparison
// anywhere in here quietly shortens honest documents — a heading list one entry
// short, a title cut mid-sentence — and no rejecting-side test would notice,
// because they all feed input that is over the limit anyway. Applying the pass
// twice must also change nothing.
func TestBoundedIngestBatchLeavesAnAlreadyBoundedBatchUnchanged(t *testing.T) {
	batch := ingestBatchAtEveryLimit()

	bounded := boundedIngestBatch(batch)
	if !reflect.DeepEqual(bounded, batch) {
		t.Fatal("a batch already on every limit must come back unchanged")
	}
	if !reflect.DeepEqual(boundedIngestBatch(bounded), bounded) {
		t.Fatal("bounding an already-bounded batch must be a no-op")
	}
}

// ingestBatchAtEveryLimit builds a batch whose every bounded field is exactly
// at its documented maximum: count limits are filled to the last slot, byte
// limits to the last byte.
func ingestBatchAtEveryLimit() IngestBatch {
	identity := maximumLengthIngestURL()
	metadataValue := strings.Repeat("m", yagocrawlcontract.MaximumDocumentMetadataBytes)
	properties := make(map[string]string, yagocrawlcontract.MaximumPropertyEntries)
	for index := range yagocrawlcontract.MaximumPropertyEntries {
		key := string(rune('a'+index%26)) + string(rune('a'+index/26)) + strings.Repeat(
			"k",
			yagocrawlcontract.MaximumDocumentMetadataBytes-2,
		)
		properties[key] = metadataValue
	}

	return IngestBatch{
		SourceURL:     identity,
		Provenance:    []byte(strings.Repeat("p", yagocrawlcontract.MaximumProvenanceBytes)),
		ProfileHandle: strings.Repeat("h", yagocrawlcontract.MaximumProfileHandleBytes),
		Document: yagocrawlcontract.DocumentIngest{
			CanonicalURL:  identity,
			NormalizedURL: identity,
			Title:         strings.Repeat("t", yagocrawlcontract.MaximumDocumentTitleBytes),
			ExtractedText: strings.Repeat("x", yagocrawlcontract.MaximumDocumentTextBytes),
			ContentHash:   metadataValue,
			Headings: repeatedValues(
				strings.Repeat("g", yagocrawlcontract.MaximumDocumentHeadingBytes),
				yagocrawlcontract.MaximumDocumentHeadings,
			),
			Outlinks: repeatedValues(identity, yagocrawlcontract.MaximumDocumentOutlinks),
			Inlinks: repeatedValues(
				yagocrawlcontract.AnchorText{URL: identity, Text: metadataValue},
				yagocrawlcontract.MaximumDocumentAnchors,
			),
			OutboundAnchors: repeatedValues(
				yagocrawlcontract.OutboundAnchor{TargetURL: identity, Text: metadataValue},
				yagocrawlcontract.MaximumDocumentAnchors,
			),
			Images: repeatedValues(
				yagocrawlcontract.ImageMetadata{URL: identity, AltText: metadataValue},
				yagocrawlcontract.MaximumDocumentImages,
			),
			Metadata: properties,
			SafetyLabels: yagocrawlcontract.SafetyLabels{
				RatingValues: repeatedValues(
					metadataValue,
					yagocrawlcontract.MaximumDocumentMetadata,
				),
			},
		},
		Postings: repeatedValues(
			yagomodel.RWIPosting{WordHash: yagomodel.Hash("ABCDEFGHIJKL")},
			yagocrawlcontract.MaximumIngestPostings,
		),
		Metadata: repeatedValues(
			yagomodel.URIMetadataRow{},
			yagocrawlcontract.MaximumMetadataRows,
		),
	}
}
