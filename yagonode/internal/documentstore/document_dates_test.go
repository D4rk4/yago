package documentstore

import (
	"math"
	"testing"
	"time"
)

func TestMergeDocumentDatesCreatesLifecycleEvidence(t *testing.T) {
	observed := time.Date(2026, 7, 10, 12, 0, 0, 0, time.FixedZone("test", 3600))
	published := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	modified := time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)
	doc := mergeDocumentDates(Document{}, Document{
		FetchedAt: observed, PublishedAt: published, ModifiedAt: modified,
		DateConfidence: 2, DateSource: " json-ld ", ContentHash: "a",
	}, false)
	if doc.FirstSeenAt != observed.UTC() || doc.ContentChangedAt != modified {
		t.Fatalf("lifecycle = %v %v", doc.FirstSeenAt, doc.ContentChangedAt)
	}
	if doc.DateConfidence != 1 || doc.DateSource != "json-ld" {
		t.Fatalf("date evidence = %v %q", doc.DateConfidence, doc.DateSource)
	}
}

func TestMergeDocumentDatesRejectsInvalidEvidence(t *testing.T) {
	observed := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	doc := mergeDocumentDates(Document{}, Document{
		IndexedAt:      observed,
		PublishedAt:    observed.Add(48 * time.Hour),
		ModifiedAt:     observed.Add(48 * time.Hour),
		DateConfidence: math.NaN(),
		DateSource:     "meta",
	}, false)
	if doc.FirstSeenAt != observed || !doc.PublishedAt.IsZero() || !doc.ModifiedAt.IsZero() {
		t.Fatalf("normalized dates = %#v", doc)
	}
	if doc.DateConfidence != 0 || doc.DateSource != "" || !doc.ContentChangedAt.IsZero() {
		t.Fatalf("invalid evidence retained = %#v", doc)
	}
}

func TestNormalizeDocumentDatesToleratesExactlyTheFutureSkew(t *testing.T) {
	// maximumFutureDateSkew is the clock-drift allowance a crawler is granted,
	// so it has to be pinned from both sides. Only the far-future case is
	// asserted elsewhere: shrinking the allowance to nothing would keep every
	// existing date test green while discarding legitimate publication dates
	// from any peer whose clock runs slightly ahead of ours.
	observed := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	atSkew := observed.Add(maximumFutureDateSkew)
	tolerated := normalizeDocumentDates(Document{
		FetchedAt:      observed,
		PublishedAt:    atSkew,
		ModifiedAt:     atSkew,
		DateConfidence: 0.5,
		DateSource:     "meta",
	})
	if !tolerated.PublishedAt.Equal(atSkew) || !tolerated.ModifiedAt.Equal(atSkew) {
		t.Fatalf("dates at the skew limit = %#v", tolerated)
	}
	if tolerated.DateConfidence != 0.5 || tolerated.DateSource != "meta" {
		t.Fatalf("evidence at the skew limit = %v %q",
			tolerated.DateConfidence, tolerated.DateSource)
	}
	// One nanosecond past the allowance is no longer drift: it is a claim about
	// the future, and it must take its confidence and source down with it.
	rejected := normalizeDocumentDates(Document{
		FetchedAt:      observed,
		PublishedAt:    atSkew.Add(time.Nanosecond),
		ModifiedAt:     atSkew.Add(time.Nanosecond),
		DateConfidence: 0.5,
		DateSource:     "meta",
	})
	if !rejected.PublishedAt.IsZero() || !rejected.ModifiedAt.IsZero() {
		t.Fatalf("dates past the skew limit = %#v", rejected)
	}
	if rejected.DateConfidence != 0 || rejected.DateSource != "" {
		t.Fatalf("evidence past the skew limit = %v %q",
			rejected.DateConfidence, rejected.DateSource)
	}
}

func TestMergeDocumentDatesDropsModificationBeforePublication(t *testing.T) {
	published := time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC)
	doc := normalizeDocumentDates(Document{
		PublishedAt:    published,
		ModifiedAt:     published.Add(-time.Hour),
		DateConfidence: -1,
	})
	if !doc.ModifiedAt.IsZero() || doc.DateConfidence != 0 {
		t.Fatalf("normalized = %#v", doc)
	}
}

func TestMergeDocumentDatesKeepsEvidenceForUnchangedContent(t *testing.T) {
	oldPublished := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	oldModified := time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC)
	firstSeen := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	previous := Document{
		ContentHash: "same", PublishedAt: oldPublished, ModifiedAt: oldModified,
		ContentChangedAt: oldModified, FirstSeenAt: firstSeen,
		DateConfidence: 0.9, DateSource: "itemprop",
	}
	incoming := Document{
		ContentHash: "same", PublishedAt: oldPublished,
		ModifiedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		DateConfidence: 1, DateSource: "json-ld",
	}
	got := mergeDocumentDates(previous, incoming, true)
	if got.FirstSeenAt != firstSeen || got.ContentChangedAt != oldModified ||
		got.ModifiedAt != oldModified || got.DateSource != "itemprop" {
		t.Fatalf("merged = %#v", got)
	}
}

func TestMergeDocumentDatesAdoptsFirstEvidenceForUnchangedLegacyContent(t *testing.T) {
	seen := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	published := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	got := mergeDocumentDates(
		Document{ExtractedText: "same", IndexedAt: seen},
		Document{ExtractedText: "same", PublishedAt: published, DateConfidence: 0.8},
		true,
	)
	if got.FirstSeenAt != seen || got.PublishedAt != published || !got.ContentChangedAt.IsZero() {
		t.Fatalf("merged = %#v", got)
	}
}

func TestMergeDocumentDatesUpdatesChangedContent(t *testing.T) {
	published := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	modified := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := mergeDocumentDates(
		Document{ContentHash: "old", PublishedAt: published},
		Document{ContentHash: "new", ModifiedAt: modified, DateConfidence: 1},
		true,
	)
	if got.PublishedAt != published || got.ContentChangedAt != modified {
		t.Fatalf("merged = %#v", got)
	}
}

func TestPublicationDateRequiresCredibleChangedEvidence(t *testing.T) {
	published := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	modified := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	when, confidence := PublicationDate(Document{
		PublishedAt: published, ModifiedAt: modified, ContentChangedAt: modified,
		DateConfidence: 0.8,
	})
	if when != modified || confidence != 0.8 {
		t.Fatalf("modified date = %v %v", when, confidence)
	}
	when, confidence = PublicationDate(Document{
		PublishedAt: published, ModifiedAt: modified, DateConfidence: 0.7,
	})
	if when != published || confidence != 0.7 {
		t.Fatalf("published date = %v %v", when, confidence)
	}
	for _, doc := range []Document{
		{PublishedAt: published},
		{ModifiedAt: modified, DateConfidence: 1},
		{FetchedAt: modified, IndexedAt: published, DateConfidence: 1},
	} {
		when, confidence = PublicationDate(doc)
		if !when.IsZero() || confidence != 0 {
			t.Fatalf("unknown date = %v %v", when, confidence)
		}
	}
}
