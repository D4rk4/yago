package documentstore

import (
	"math"
	"strings"
	"time"
)

const maximumFutureDateSkew = 24 * time.Hour

func mergeDocumentDates(previous Document, incoming Document, found bool) Document {
	incoming = normalizeDocumentDates(incoming)
	if !found {
		if incoming.FirstSeenAt.IsZero() {
			incoming.FirstSeenAt = observationTime(incoming)
		}
		incoming.ContentChangedAt = declaredChangeTime(incoming)

		return incoming
	}

	previous = normalizeDocumentDates(previous)
	incoming.FirstSeenAt = previous.FirstSeenAt
	if incoming.FirstSeenAt.IsZero() {
		incoming.FirstSeenAt = observationTime(previous)
	}
	if sameDocumentContent(previous, incoming) {
		if hasDateEvidence(previous) {
			incoming.PublishedAt = previous.PublishedAt
			incoming.ModifiedAt = previous.ModifiedAt
			incoming.DateConfidence = previous.DateConfidence
			incoming.DateSource = previous.DateSource
		}
		incoming.ContentChangedAt = previous.ContentChangedAt

		return incoming
	}

	if incoming.PublishedAt.IsZero() {
		incoming.PublishedAt = previous.PublishedAt
	}
	incoming.ContentChangedAt = declaredChangeTime(incoming)

	return incoming
}

func normalizeDocumentDates(doc Document) Document {
	doc.FetchedAt = doc.FetchedAt.UTC()
	doc.IndexedAt = doc.IndexedAt.UTC()
	doc.PublishedAt = doc.PublishedAt.UTC()
	doc.ModifiedAt = doc.ModifiedAt.UTC()
	doc.FirstSeenAt = doc.FirstSeenAt.UTC()
	doc.ContentChangedAt = doc.ContentChangedAt.UTC()
	doc.DateSource = strings.TrimSpace(doc.DateSource)
	if math.IsNaN(doc.DateConfidence) || doc.DateConfidence < 0 {
		doc.DateConfidence = 0
	}
	if doc.DateConfidence > 1 {
		doc.DateConfidence = 1
	}
	observed := observationTime(doc)
	if !observed.IsZero() {
		latest := observed.Add(maximumFutureDateSkew)
		if doc.PublishedAt.After(latest) {
			doc.PublishedAt = time.Time{}
		}
		if doc.ModifiedAt.After(latest) {
			doc.ModifiedAt = time.Time{}
		}
	}
	if !doc.PublishedAt.IsZero() && doc.ModifiedAt.Before(doc.PublishedAt) {
		doc.ModifiedAt = time.Time{}
	}
	if doc.PublishedAt.IsZero() && doc.ModifiedAt.IsZero() {
		doc.DateConfidence = 0
		doc.DateSource = ""
	}

	return doc
}

func observationTime(doc Document) time.Time {
	if !doc.FetchedAt.IsZero() {
		return doc.FetchedAt
	}

	return doc.IndexedAt
}

func declaredChangeTime(doc Document) time.Time {
	if !doc.ModifiedAt.IsZero() {
		return doc.ModifiedAt
	}

	return doc.PublishedAt
}

func sameDocumentContent(previous Document, incoming Document) bool {
	if previous.ContentHash != "" && incoming.ContentHash != "" {
		return previous.ContentHash == incoming.ContentHash
	}

	return previous.ExtractedText == incoming.ExtractedText
}

func hasDateEvidence(doc Document) bool {
	return !doc.PublishedAt.IsZero() || !doc.ModifiedAt.IsZero()
}

func PublicationDate(doc Document) (time.Time, float64) {
	if doc.DateConfidence <= 0 {
		return time.Time{}, 0
	}
	if !doc.ModifiedAt.IsZero() && doc.ModifiedAt.Equal(doc.ContentChangedAt) {
		return doc.ModifiedAt, doc.DateConfidence
	}
	if !doc.PublishedAt.IsZero() {
		return doc.PublishedAt, doc.DateConfidence
	}

	return time.Time{}, 0
}
