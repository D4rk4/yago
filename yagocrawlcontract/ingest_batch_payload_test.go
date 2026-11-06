package yagocrawlcontract

import (
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func notModifiedBatch() IngestBatch {
	return IngestBatch{
		SourceURL:     "https://example.org/a",
		Provenance:    []byte("order-a"),
		ProfileHandle: "profile-a",
		ObservationID: "observation-a",
		ObservedAt:    time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC),
		ResponseValidators: &HTTPValidators{
			ETag:         `W/"v2"`,
			LastModified: "Sun, 06 Nov 1994 08:49:37 GMT",
		},
		NotModified: &NotModifiedObservation{Revision: storedDocumentRevision()},
	}
}

func documentBatch() IngestBatch {
	return IngestBatch{
		SourceURL:     "https://example.org/a",
		ObservationID: "observation-a",
		Document: DocumentIngest{
			NormalizedURL: "https://example.org/a",
			ExtractedText: "body text",
		},
		Postings: []yagomodel.RWIPosting{{}},
		Metadata: []yagomodel.URIMetadataRow{{}},
		ResponseValidators: &HTTPValidators{
			ETag: `"v1"`,
		},
	}
}

func boundedEmptyDocument() DocumentIngest {
	return DocumentIngest{
		Headings:        []string{},
		Outlinks:        []string{},
		Inlinks:         []AnchorText{},
		OutboundAnchors: []OutboundAnchor{},
		Images:          []ImageMetadata{},
		SafetyLabels:    SafetyLabels{RatingValues: []string{}},
	}
}

func TestIngestBatchAcceptsEachSinglePayload(t *testing.T) {
	legacyRemoval := IngestBatch{
		SourceURL:     "https://example.org/gone",
		ObservationID: "observation-a",
		Removed:       true,
		Document:      boundedEmptyDocument(),
		Postings:      []yagomodel.RWIPosting{},
		Metadata:      []yagomodel.URIMetadataRow{},
	}
	boundedNotModified := notModifiedBatch()
	boundedNotModified.Document = boundedEmptyDocument()
	boundedNotModified.Postings = []yagomodel.RWIPosting{}
	boundedNotModified.Metadata = []yagomodel.URIMetadataRow{}
	withoutResponseValidators := notModifiedBatch()
	withoutResponseValidators.ResponseValidators = nil
	accepted := map[string]IngestBatch{
		"legacy document": {
			SourceURL: "https://example.org/a",
			Document:  documentBatch().Document,
		},
		"document with validators":        documentBatch(),
		"legacy removal":                  legacyRemoval,
		"source only":                     {SourceURL: "https://example.org/a"},
		"not modified":                    notModifiedBatch(),
		"bounded not modified":            boundedNotModified,
		"not modified without validators": withoutResponseValidators,
	}
	for name, batch := range accepted {
		if err := batch.ValidatePayload(); err != nil {
			t.Errorf("%s: ValidatePayload() = %v", name, err)
		}
	}
}

func TestIngestBatchRefusesMixedPayloads(t *testing.T) {
	familyFriendly := true
	mixed := map[string]func(*IngestBatch){
		"not modified removal": func(batch *IngestBatch) { batch.Removed = true },
		"not modified title":   func(batch *IngestBatch) { batch.Document.Title = "Title" },
		"not modified fetch time": func(batch *IngestBatch) {
			batch.Document.FetchedAt = time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
		},
		"not modified headings": func(batch *IngestBatch) {
			batch.Document.Headings = []string{"Heading"}
		},
		"not modified properties": func(batch *IngestBatch) {
			batch.Document.Metadata = map[string]string{"key": "value"}
		},
		"not modified safety label": func(batch *IngestBatch) {
			batch.Document.SafetyLabels.FamilyFriendly = &familyFriendly
		},
		"not modified rating": func(batch *IngestBatch) {
			batch.Document.SafetyLabels.RatingValues = []string{"general"}
		},
		"not modified postings": func(batch *IngestBatch) {
			batch.Postings = []yagomodel.RWIPosting{{}}
		},
		"not modified metadata rows": func(batch *IngestBatch) {
			batch.Metadata = []yagomodel.URIMetadataRow{{}}
		},
	}
	for name, change := range mixed {
		batch := notModifiedBatch()
		change(&batch)
		if err := batch.ValidatePayload(); !errors.Is(err, errIngestPayloadMixed) {
			t.Errorf("%s: ValidatePayload() = %v, want %v", name, err, errIngestPayloadMixed)
		}
	}
	removalWithDocument := documentBatch()
	removalWithDocument.Removed = true
	removalWithPostings := IngestBatch{
		SourceURL: "https://example.org/gone",
		Removed:   true,
		Postings:  []yagomodel.RWIPosting{{}},
	}
	for name, batch := range map[string]IngestBatch{
		"removal document": removalWithDocument,
		"removal postings": removalWithPostings,
	} {
		if err := batch.ValidatePayload(); !errors.Is(err, errIngestPayloadMixed) {
			t.Errorf("%s: ValidatePayload() = %v, want %v", name, err, errIngestPayloadMixed)
		}
	}
}

func TestIngestBatchRefusesNotModifiedWithoutRequestIdentity(t *testing.T) {
	missing := map[string]struct {
		change func(*IngestBatch)
		want   error
	}{
		"source URL": {
			func(batch *IngestBatch) { batch.SourceURL = "" },
			errNotModifiedIdentity,
		},
		"observation ID": {
			func(batch *IngestBatch) { batch.ObservationID = "" },
			errNotModifiedIdentity,
		},
		"empty observation": {
			func(batch *IngestBatch) { batch.NotModified = &NotModifiedObservation{} },
			errDocumentRevisionIncomplete,
		},
		"revision fetch time": {
			func(batch *IngestBatch) { batch.NotModified.Revision.FetchedAt = time.Time{} },
			errDocumentRevisionIncomplete,
		},
	}
	for name, test := range missing {
		batch := notModifiedBatch()
		test.change(&batch)
		if err := batch.ValidatePayload(); !errors.Is(err, test.want) {
			t.Errorf("%s: ValidatePayload() = %v, want %v", name, err, test.want)
		}
	}
}

func TestIngestBatchRefusesInvalidResponseValidators(t *testing.T) {
	invalid := map[string]struct {
		validators HTTPValidators
		want       error
	}{
		"empty":       {HTTPValidators{}, errHTTPValidatorsEmpty},
		"entity tag":  {HTTPValidators{ETag: "v2"}, errEntityTagSyntax},
		"date":        {HTTPValidators{LastModified: "Sun, 06 Nov 1994"}, errHTTPDateSyntax},
		"line breaks": {HTTPValidators{ETag: "\"v2\"\r\n"}, errEntityTagSyntax},
	}
	for name, test := range invalid {
		for kind, batch := range map[string]IngestBatch{
			"document":     documentBatch(),
			"not modified": notModifiedBatch(),
		} {
			validators := test.validators
			batch.ResponseValidators = &validators
			if err := batch.ValidatePayload(); !errors.Is(err, test.want) {
				t.Errorf("%s %s: ValidatePayload() = %v, want %v", kind, name, err, test.want)
			}
		}
	}
}
