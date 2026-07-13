package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

var (
	errIngestBatchTooLarge    = errors.New("ingest batch exceeds transport limit")
	errIngestIdentityTooLarge = errors.New("ingest identity exceeds URL limit")
)

func prepareIngestMessage(batch IngestBatch) ([]byte, error) {
	if err := validateIngestIdentity(batch); err != nil {
		return nil, err
	}
	batch = boundedIngestBatch(batch)
	data, err := yagocrawlcontract.MarshalIngestBatch(batch)
	if err != nil {
		return nil, fmt.Errorf("marshal bounded ingest batch: %w", err)
	}
	if len(data) <= yagocrawlcontract.MaximumIngestBatchBytes {
		return data, nil
	}

	postings := batch.Postings
	batch.Postings = nil
	batch, data, err = fitIngestDocument(batch)
	if err != nil {
		return nil, err
	}
	best := data
	lower, upper := 0, len(postings)
	for lower <= upper {
		middle := lower + (upper-lower)/2
		batch.Postings = postings[:middle]
		candidate := encodedValidatedIngestBatch(batch)
		if len(candidate) <= yagocrawlcontract.MaximumIngestBatchBytes {
			best = candidate
			lower = middle + 1
		} else {
			upper = middle - 1
		}
	}

	return best, nil
}

func fitIngestDocument(
	batch IngestBatch,
) (IngestBatch, []byte, error) {
	for {
		data := encodedValidatedIngestBatch(batch)
		if len(data) <= yagocrawlcontract.MaximumIngestBatchBytes {
			return batch, data, nil
		}

		sizes := []int{
			encodedCollectionSize(batch.Document.Outlinks),
			encodedCollectionSize(batch.Document.OutboundAnchors),
			encodedCollectionSize(batch.Document.Inlinks),
			encodedCollectionSize(batch.Document.Headings),
			encodedCollectionSize(batch.Document.Images),
			encodedCollectionSize(batch.Metadata),
		}
		largest := -1
		largestSize := 0
		for index, size := range sizes {
			if size > 2 && size > largestSize {
				largest = index
				largestSize = size
			}
		}
		switch largest {
		case 0:
			batch.Document.Outlinks = halve(batch.Document.Outlinks)
		case 1:
			batch.Document.OutboundAnchors = halve(batch.Document.OutboundAnchors)
		case 2:
			batch.Document.Inlinks = halve(batch.Document.Inlinks)
		case 3:
			batch.Document.Headings = halve(batch.Document.Headings)
		case 4:
			batch.Document.Images = halve(batch.Document.Images)
		case 5:
			batch.Metadata = halve(batch.Metadata)
		default:
			return IngestBatch{}, nil, fmt.Errorf(
				"%w: %d bytes",
				errIngestBatchTooLarge,
				len(data),
			)
		}
	}
}

func encodedValidatedIngestBatch(batch IngestBatch) []byte {
	data, _ := yagocrawlcontract.MarshalIngestBatch(batch)

	return data
}

func boundedIngestBatch(batch IngestBatch) IngestBatch {
	batch.ProfileHandle = boundedIngestText(
		batch.ProfileHandle,
		yagocrawlcontract.MaximumProfileHandleBytes,
	)
	batch.Provenance = slices.Clone(
		batch.Provenance[:min(
			len(batch.Provenance),
			yagocrawlcontract.MaximumProvenanceBytes,
		)],
	)
	batch.Document = boundedIngestDocument(batch.Document)
	batch.Postings = boundedIngestPostings(batch.Postings)
	batch.Metadata = boundedIngestMetadata(batch.Metadata)

	return batch
}

func boundedIngestDocument(
	document yagocrawlcontract.DocumentIngest,
) yagocrawlcontract.DocumentIngest {
	document.Title = boundedIngestText(
		document.Title,
		yagocrawlcontract.MaximumDocumentTitleBytes,
	)
	document.RawContentReference = boundedIngestText(
		document.RawContentReference,
		yagocrawlcontract.MaximumDocumentMetadataBytes,
	)
	document.Language = boundedIngestText(
		document.Language,
		yagocrawlcontract.MaximumDocumentMetadataBytes,
	)
	document.ContentType = boundedIngestText(
		document.ContentType,
		yagocrawlcontract.MaximumDocumentMetadataBytes,
	)
	document.FetchStatus = boundedIngestText(
		document.FetchStatus,
		yagocrawlcontract.MaximumDocumentMetadataBytes,
	)
	document.DateSource = boundedIngestText(
		document.DateSource,
		yagocrawlcontract.MaximumDocumentMetadataBytes,
	)
	document.ContentHash = boundedIngestText(
		document.ContentHash,
		yagocrawlcontract.MaximumDocumentMetadataBytes,
	)
	document.ExtractedText = boundedIngestText(
		document.ExtractedText,
		yagocrawlcontract.MaximumDocumentTextBytes,
	)
	document.Headings = boundedIngestStrings(
		document.Headings,
		yagocrawlcontract.MaximumDocumentHeadings,
		yagocrawlcontract.MaximumDocumentHeadingBytes,
	)
	document.Outlinks = boundedIngestURLs(
		document.Outlinks,
		yagocrawlcontract.MaximumDocumentOutlinks,
	)
	document.Inlinks = boundedIngestInboundAnchors(document.Inlinks)
	document.OutboundAnchors = boundedIngestOutboundAnchors(document.OutboundAnchors)
	document.Images = boundedIngestImages(document.Images)
	document.Metadata = boundedIngestProperties(document.Metadata)
	document.SafetyLabels.RatingValues = boundedIngestStrings(
		document.SafetyLabels.RatingValues,
		yagocrawlcontract.MaximumDocumentMetadata,
		yagocrawlcontract.MaximumDocumentMetadataBytes,
	)

	return document
}

func validateIngestIdentity(batch IngestBatch) error {
	identities := []struct {
		name  string
		value string
	}{
		{"source URL", batch.SourceURL},
		{"canonical URL", batch.Document.CanonicalURL},
		{"normalized URL", batch.Document.NormalizedURL},
	}
	for _, identity := range identities {
		if len(identity.value) > yagocrawlcontract.MaximumCrawlURLBytes {
			return fmt.Errorf(
				"%w: %s is %d bytes",
				errIngestIdentityTooLarge,
				identity.name,
				len(identity.value),
			)
		}
	}

	return nil
}

func boundedIngestPostings(postings []yagomodel.RWIPosting) []yagomodel.RWIPosting {
	postings = postings[:min(len(postings), yagocrawlcontract.MaximumIngestPostings)]
	bounded := make([]yagomodel.RWIPosting, len(postings))
	for index, posting := range postings {
		posting.Properties = boundedIngestProperties(posting.Properties)
		bounded[index] = posting
	}

	return bounded
}

func boundedIngestMetadata(rows []yagomodel.URIMetadataRow) []yagomodel.URIMetadataRow {
	rows = rows[:min(len(rows), yagocrawlcontract.MaximumMetadataRows)]
	bounded := make([]yagomodel.URIMetadataRow, len(rows))
	for index, row := range rows {
		row.Properties = boundedIngestProperties(row.Properties)
		bounded[index] = row
	}

	return bounded
}

func boundedIngestInboundAnchors(
	anchors []yagocrawlcontract.AnchorText,
) []yagocrawlcontract.AnchorText {
	bounded := make(
		[]yagocrawlcontract.AnchorText,
		0,
		min(len(anchors), yagocrawlcontract.MaximumDocumentAnchors),
	)
	for _, anchor := range anchors {
		if len(anchor.URL) > yagocrawlcontract.MaximumCrawlURLBytes {
			continue
		}
		anchor.Text = boundedIngestText(
			anchor.Text,
			yagocrawlcontract.MaximumDocumentMetadataBytes,
		)
		bounded = append(bounded, anchor)
		if len(bounded) == yagocrawlcontract.MaximumDocumentAnchors {
			break
		}
	}

	return bounded
}

func boundedIngestOutboundAnchors(
	anchors []yagocrawlcontract.OutboundAnchor,
) []yagocrawlcontract.OutboundAnchor {
	bounded := make(
		[]yagocrawlcontract.OutboundAnchor,
		0,
		min(len(anchors), yagocrawlcontract.MaximumDocumentAnchors),
	)
	for _, anchor := range anchors {
		if len(anchor.TargetURL) > yagocrawlcontract.MaximumCrawlURLBytes {
			continue
		}
		anchor.Text = boundedIngestText(
			anchor.Text,
			yagocrawlcontract.MaximumDocumentMetadataBytes,
		)
		bounded = append(bounded, anchor)
		if len(bounded) == yagocrawlcontract.MaximumDocumentAnchors {
			break
		}
	}

	return bounded
}

func boundedIngestImages(
	images []yagocrawlcontract.ImageMetadata,
) []yagocrawlcontract.ImageMetadata {
	bounded := make(
		[]yagocrawlcontract.ImageMetadata,
		0,
		min(len(images), yagocrawlcontract.MaximumDocumentImages),
	)
	for _, image := range images {
		if len(image.URL) > yagocrawlcontract.MaximumCrawlURLBytes {
			continue
		}
		image.AltText = boundedIngestText(
			image.AltText,
			yagocrawlcontract.MaximumDocumentMetadataBytes,
		)
		bounded = append(bounded, image)
		if len(bounded) == yagocrawlcontract.MaximumDocumentImages {
			break
		}
	}

	return bounded
}

func boundedIngestProperties(properties map[string]string) map[string]string {
	if len(properties) == 0 {
		return nil
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	keys = keys[:min(len(keys), yagocrawlcontract.MaximumPropertyEntries)]
	bounded := make(map[string]string, len(keys))
	for _, key := range keys {
		bounded[boundedIngestText(
			key,
			yagocrawlcontract.MaximumDocumentMetadataBytes,
		)] = boundedIngestText(
			properties[key],
			yagocrawlcontract.MaximumDocumentMetadataBytes,
		)
	}

	return bounded
}

func boundedIngestStrings(values []string, maximum, maximumBytes int) []string {
	values = values[:min(len(values), maximum)]
	bounded := make([]string, len(values))
	for index, value := range values {
		bounded[index] = boundedIngestText(value, maximumBytes)
	}

	return bounded
}

func boundedIngestURLs(values []string, maximum int) []string {
	bounded := make([]string, 0, min(len(values), maximum))
	for _, value := range values {
		if len(value) > yagocrawlcontract.MaximumCrawlURLBytes {
			continue
		}
		bounded = append(bounded, value)
		if len(bounded) == maximum {
			break
		}
	}

	return bounded
}

func boundedIngestText(text string, maximum int) string {
	if len(text) <= maximum {
		return text
	}
	cut := maximum
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	return text[:cut]
}

func encodedSize(value any) int {
	data, _ := json.Marshal(value)

	return len(data)
}

func encodedCollectionSize[T any](values []T) int {
	if len(values) == 0 {
		return 0
	}

	return encodedSize(values)
}

func halve[T any](values []T) []T {
	if len(values) <= 1 {
		return nil
	}

	return values[:len(values)/2]
}
