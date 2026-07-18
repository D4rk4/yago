package ingest

import (
	"errors"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

func TestPrepareIngestMessageFitsTransportLimit(t *testing.T) {
	postings := make([]yagomodel.RWIPosting, 600)
	for index := range postings {
		postings[index] = yagomodel.RWIPosting{
			WordHash: yagomodel.Hash("ABCDEFGHIJKL"),
			Properties: map[string]string{
				yagomodel.ColURLHash: strings.Repeat(
					"u",
					yagocrawlcontract.MaximumDocumentMetadataBytes,
				),
			},
		}
	}
	outlinks := make([]string, 2200)
	for index := range outlinks {
		outlinks[index] = "https://example.org/" + strings.Repeat("x", 2000)
	}
	batch := IngestBatch{
		SourceURL: "https://example.org/large",
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL: "https://example.org/large",
			ExtractedText: strings.Repeat("content ", 200_000),
			Outlinks:      outlinks,
		},
		Postings: postings,
	}

	data, err := prepareIngestMessage(batch)
	if err != nil {
		t.Fatalf("prepare ingest: %v", err)
	}
	if len(data) > yagocrawlcontract.MaximumIngestBatchBytes {
		t.Fatalf("payload bytes = %d", len(data))
	}
	decoded, err := yagocrawlcontract.UnmarshalIngestBatch(data)
	if err != nil {
		t.Fatalf("decode prepared ingest: %v", err)
	}
	if len(decoded.Document.ExtractedText) != yagocrawlcontract.MaximumDocumentTextBytes {
		t.Fatalf("text bytes = %d", len(decoded.Document.ExtractedText))
	}
	if len(decoded.Postings) == 0 || len(decoded.Postings) >= len(postings) {
		t.Fatalf("fitted postings = %d", len(decoded.Postings))
	}
	if len(decoded.Document.Outlinks) >= len(outlinks) {
		t.Fatalf("fitted outlinks = %d", len(decoded.Document.Outlinks))
	}
}

func TestBoundedIngestBatchCopiesAndPreservesUTF8(t *testing.T) {
	properties := map[string]string{"key": strings.Repeat("я", 5000)}
	overlongURL := "https://example.org/" + strings.Repeat(
		"x",
		yagocrawlcontract.MaximumCrawlURLBytes,
	)
	batch := IngestBatch{
		Provenance:    []byte("source"),
		ProfileHandle: strings.Repeat("п", 300),
		Document: yagocrawlcontract.DocumentIngest{
			Title:    "a" + strings.Repeat("я", 3000),
			Headings: []string{"heading"},
			Metadata: properties,
			Outlinks: []string{overlongURL, "https://example.org/out"},
			Inlinks: []yagocrawlcontract.AnchorText{
				{URL: overlongURL},
				{URL: "https://example.org/in"},
			},
			OutboundAnchors: []yagocrawlcontract.OutboundAnchor{
				{TargetURL: overlongURL},
				{TargetURL: "https://example.org/anchor"},
			},
			Images: []yagocrawlcontract.ImageMetadata{
				{URL: overlongURL},
				{URL: "https://example.org/image.png"},
			},
		},
		Postings: []yagomodel.RWIPosting{{Properties: properties}},
		Metadata: []yagomodel.URIMetadataRow{{Properties: properties}},
	}
	bounded := boundedIngestBatch(batch)
	batch.Provenance[0] = 'X'
	properties["key"] = "changed"
	if string(bounded.Provenance) != "source" {
		t.Fatalf("provenance = %q", bounded.Provenance)
	}
	for _, value := range []string{
		bounded.ProfileHandle,
		bounded.Document.Title,
		bounded.Document.Headings[0],
		bounded.Document.Metadata["key"],
		bounded.Postings[0].Properties["key"],
		bounded.Metadata[0].Properties["key"],
	} {
		if !utf8.ValidString(value) {
			t.Fatalf("invalid UTF-8 boundary: %q", value)
		}
	}
	if len(bounded.Document.Outlinks) != 1 ||
		bounded.Document.Outlinks[0] != "https://example.org/out" ||
		len(bounded.Document.Inlinks) != 1 ||
		bounded.Document.Inlinks[0].URL != "https://example.org/in" ||
		len(bounded.Document.OutboundAnchors) != 1 ||
		bounded.Document.OutboundAnchors[0].TargetURL != "https://example.org/anchor" ||
		len(bounded.Document.Images) != 1 ||
		bounded.Document.Images[0].URL != "https://example.org/image.png" {
		t.Fatalf("bounded URL collections = %#v", bounded.Document)
	}
}

func TestPrepareIngestMessageReturnsMarshalFailure(t *testing.T) {
	_, err := prepareIngestMessage(IngestBatch{Document: yagocrawlcontract.DocumentIngest{
		DateConfidence: math.NaN(),
	}})
	if err == nil {
		t.Fatal("expected JSON marshal failure")
	}
}

func TestPrepareIngestMessageRejectsOverlongIdentity(t *testing.T) {
	_, err := prepareIngestMessage(IngestBatch{
		SourceURL: "https://example.org/" + strings.Repeat(
			"x",
			yagocrawlcontract.MaximumCrawlURLBytes,
		),
	})
	if !errors.Is(err, errIngestIdentityTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareIngestMessageFitsOversizedEscapedScalar(t *testing.T) {
	data, err := prepareIngestMessage(IngestBatch{Document: yagocrawlcontract.DocumentIngest{
		ExtractedText: strings.Repeat("\x00", yagocrawlcontract.MaximumDocumentTextBytes),
	}})
	if err != nil {
		t.Fatalf("prepare ingest: %v", err)
	}
	if len(data) > yagocrawlcontract.MaximumIngestBatchBytes {
		t.Fatalf("payload bytes = %d", len(data))
	}
	decoded, err := yagocrawlcontract.UnmarshalIngestBatch(data)
	if err != nil {
		t.Fatalf("decode prepared ingest: %v", err)
	}
	if len(decoded.Document.ExtractedText) == 0 ||
		len(decoded.Document.ExtractedText) >= yagocrawlcontract.MaximumDocumentTextBytes {
		t.Fatalf("fitted text bytes = %d", len(decoded.Document.ExtractedText))
	}
}

func TestPrepareIngestMessageFitsEscapedTextAtUTF8Boundary(t *testing.T) {
	text := strings.Repeat("я\x00\x00\x00", yagocrawlcontract.MaximumDocumentTextBytes/5)
	data, err := prepareIngestMessage(IngestBatch{Document: yagocrawlcontract.DocumentIngest{
		ExtractedText: text,
	}})
	if err != nil {
		t.Fatalf("prepare ingest: %v", err)
	}
	decoded, err := yagocrawlcontract.UnmarshalIngestBatch(data)
	if err != nil {
		t.Fatalf("decode prepared ingest: %v", err)
	}
	if !utf8.ValidString(decoded.Document.ExtractedText) ||
		!strings.HasPrefix(text, decoded.Document.ExtractedText) ||
		len(decoded.Document.ExtractedText) >= len(text) {
		t.Fatalf("fitted text is not a valid source prefix")
	}
}

func TestPrepareIngestMessageRejectsOversizedRequiredScalar(t *testing.T) {
	_, err := prepareIngestMessage(IngestBatch{
		ObservationID: strings.Repeat("\x00", yagocrawlcontract.MaximumDocumentTextBytes),
	})
	if !errors.Is(err, errIngestBatchTooLarge) {
		t.Fatalf("error = %v, want transport limit", err)
	}
}

func TestHalvePropertiesIsDeterministicAndDropsSingleton(t *testing.T) {
	if halved := halveProperties(map[string]string{"only": "value"}); halved != nil {
		t.Fatalf("singleton properties = %v, want nil", halved)
	}
	halved := halveProperties(map[string]string{
		"charlie": "3",
		"alpha":   "1",
		"bravo":   "2",
	})
	if len(halved) != 1 || halved["alpha"] != "1" {
		t.Fatalf("halved properties = %v", halved)
	}
}

type ingestCollectionFitCase struct {
	name             string
	batch            IngestBatch
	collectionLength func(IngestBatch) int
}

func TestFitIngestDocumentReducesLargestBoundedCollection(t *testing.T) {
	for _, test := range ingestCollectionFitCases() {
		t.Run(test.name, func(t *testing.T) {
			bounded := boundedIngestBatch(test.batch)
			initial := encodedValidatedIngestBatch(bounded)
			if len(initial) <= yagocrawlcontract.MaximumIngestBatchBytes {
				t.Fatalf("initial payload bytes = %d", len(initial))
			}
			before := test.collectionLength(bounded)
			fitted, data, err := fitIngestDocument(bounded)
			if err != nil {
				t.Fatalf("fit ingest document: %v", err)
			}
			if len(data) > yagocrawlcontract.MaximumIngestBatchBytes {
				t.Fatalf("payload bytes = %d", len(data))
			}
			if got := test.collectionLength(fitted); got >= before {
				t.Fatalf("fitted collection length = %d, initial = %d", got, before)
			}
		})
	}
}

func ingestCollectionFitCases() []ingestCollectionFitCase {
	largeURL := maximumLengthIngestURL()
	escapedMetadata := strings.Repeat(
		"\x00",
		yagocrawlcontract.MaximumDocumentMetadataBytes,
	)

	return []ingestCollectionFitCase{
		outlinksFitCase(largeURL),
		outboundAnchorsFitCase(largeURL),
		inboundAnchorsFitCase(largeURL),
		headingsFitCase(),
		imagesFitCase(largeURL, escapedMetadata),
		metadataFitCase(escapedMetadata),
		documentMetadataFitCase(escapedMetadata),
		safetyRatingsFitCase(escapedMetadata),
	}
}

func outlinksFitCase(largeURL string) ingestCollectionFitCase {
	return ingestCollectionFitCase{
		name: "outlinks",
		batch: IngestBatch{Document: yagocrawlcontract.DocumentIngest{
			Outlinks: repeatedValues(
				largeURL,
				yagocrawlcontract.MaximumDocumentOutlinks,
			),
		}},
		collectionLength: func(batch IngestBatch) int {
			return len(batch.Document.Outlinks)
		},
	}
}

func outboundAnchorsFitCase(largeURL string) ingestCollectionFitCase {
	return ingestCollectionFitCase{
		name: "outbound anchors",
		batch: IngestBatch{Document: yagocrawlcontract.DocumentIngest{
			OutboundAnchors: repeatedValues(
				yagocrawlcontract.OutboundAnchor{
					TargetURL: largeURL,
					Text: strings.Repeat(
						"x",
						yagocrawlcontract.MaximumDocumentMetadataBytes,
					),
				},
				yagocrawlcontract.MaximumDocumentAnchors,
			),
		}},
		collectionLength: func(batch IngestBatch) int {
			return len(batch.Document.OutboundAnchors)
		},
	}
}

func inboundAnchorsFitCase(largeURL string) ingestCollectionFitCase {
	return ingestCollectionFitCase{
		name: "inbound anchors",
		batch: IngestBatch{Document: yagocrawlcontract.DocumentIngest{
			Inlinks: repeatedValues(
				yagocrawlcontract.AnchorText{
					URL: largeURL,
					Text: strings.Repeat(
						"x",
						yagocrawlcontract.MaximumDocumentMetadataBytes,
					),
				},
				yagocrawlcontract.MaximumDocumentAnchors,
			),
		}},
		collectionLength: func(batch IngestBatch) int {
			return len(batch.Document.Inlinks)
		},
	}
}

func headingsFitCase() ingestCollectionFitCase {
	return ingestCollectionFitCase{
		name: "headings",
		batch: IngestBatch{Document: yagocrawlcontract.DocumentIngest{
			ExtractedText: strings.Repeat("\x00", 200_000),
			Headings: repeatedValues(
				strings.Repeat(
					"\x00",
					yagocrawlcontract.MaximumDocumentHeadingBytes,
				),
				yagocrawlcontract.MaximumDocumentHeadings,
			),
		}},
		collectionLength: func(batch IngestBatch) int {
			return len(batch.Document.Headings)
		},
	}
}

func imagesFitCase(
	largeURL string,
	escapedMetadata string,
) ingestCollectionFitCase {
	return ingestCollectionFitCase{
		name: "images",
		batch: IngestBatch{Document: yagocrawlcontract.DocumentIngest{
			ExtractedText: strings.Repeat("\x00", 450_000),
			Images: repeatedValues(
				yagocrawlcontract.ImageMetadata{
					URL:     largeURL,
					AltText: escapedMetadata,
				},
				yagocrawlcontract.MaximumDocumentImages,
			),
		}},
		collectionLength: func(batch IngestBatch) int {
			return len(batch.Document.Images)
		},
	}
}

func metadataFitCase(escapedMetadata string) ingestCollectionFitCase {
	properties := make(map[string]string, yagocrawlcontract.MaximumPropertyEntries)
	for index := range yagocrawlcontract.MaximumPropertyEntries {
		key := string(rune(33+index)) + strings.Repeat(
			"\x00",
			yagocrawlcontract.MaximumDocumentMetadataBytes-1,
		)
		properties[key] = escapedMetadata
	}

	return ingestCollectionFitCase{
		name: "metadata",
		batch: IngestBatch{Metadata: []yagomodel.URIMetadataRow{{
			Properties: properties,
		}}},
		collectionLength: func(batch IngestBatch) int {
			return len(batch.Metadata)
		},
	}
}

func documentMetadataFitCase(escapedMetadata string) ingestCollectionFitCase {
	properties := make(map[string]string, yagocrawlcontract.MaximumPropertyEntries)
	for index := range yagocrawlcontract.MaximumPropertyEntries {
		key := string(rune(33+index)) + strings.Repeat(
			"\x00",
			yagocrawlcontract.MaximumDocumentMetadataBytes-1,
		)
		properties[key] = escapedMetadata
	}

	return ingestCollectionFitCase{
		name: "document metadata",
		batch: IngestBatch{Document: yagocrawlcontract.DocumentIngest{
			Metadata: properties,
		}},
		collectionLength: func(batch IngestBatch) int {
			return len(batch.Document.Metadata)
		},
	}
}

func safetyRatingsFitCase(escapedMetadata string) ingestCollectionFitCase {
	return ingestCollectionFitCase{
		name: "safety ratings",
		batch: IngestBatch{Document: yagocrawlcontract.DocumentIngest{
			ExtractedText: strings.Repeat("\x00", 500_000),
			SafetyLabels: yagocrawlcontract.SafetyLabels{
				RatingValues: repeatedValues(
					escapedMetadata,
					yagocrawlcontract.MaximumDocumentMetadata,
				),
			},
		}},
		collectionLength: func(batch IngestBatch) int {
			return len(batch.Document.SafetyLabels.RatingValues)
		},
	}
}

func maximumLengthIngestURL() string {
	const prefix = "https://example.org/"

	return prefix + strings.Repeat(
		"x",
		yagocrawlcontract.MaximumCrawlURLBytes-len(prefix),
	)
}

func TestBoundedIngestCollectionsStopAtLimits(t *testing.T) {
	validURL := "https://example.org/item"
	inbound := repeatedValues(
		yagocrawlcontract.AnchorText{URL: validURL},
		yagocrawlcontract.MaximumDocumentAnchors+1,
	)
	outbound := repeatedValues(
		yagocrawlcontract.OutboundAnchor{TargetURL: validURL},
		yagocrawlcontract.MaximumDocumentAnchors+1,
	)
	images := repeatedValues(
		yagocrawlcontract.ImageMetadata{URL: validURL},
		yagocrawlcontract.MaximumDocumentImages+1,
	)
	urls := repeatedValues(validURL, yagocrawlcontract.MaximumDocumentOutlinks+1)
	wantAnchors := yagocrawlcontract.MaximumDocumentAnchors
	wantImages := yagocrawlcontract.MaximumDocumentImages
	wantURLs := yagocrawlcontract.MaximumDocumentOutlinks

	if got := len(boundedIngestInboundAnchors(inbound)); got != wantAnchors {
		t.Fatalf("inbound anchors = %d", got)
	}
	if got := len(boundedIngestOutboundAnchors(outbound)); got != wantAnchors {
		t.Fatalf("outbound anchors = %d", got)
	}
	if got := len(boundedIngestImages(images)); got != wantImages {
		t.Fatalf("images = %d", got)
	}
	if got := len(boundedIngestURLs(urls, wantURLs)); got != wantURLs {
		t.Fatalf("URLs = %d", got)
	}
}

func TestBoundedIngestTextPreservesSplitRune(t *testing.T) {
	bounded := boundedIngestText("a"+strings.Repeat("я", 3), 4)
	if bounded != "aя" {
		t.Fatalf("bounded text = %q", bounded)
	}
}

func repeatedValues[T any](value T, length int) []T {
	values := make([]T, length)
	for index := range values {
		values[index] = value
	}

	return values
}
