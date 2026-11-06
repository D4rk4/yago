package yagocrawlcontract

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func storedDocumentRevision() DocumentRevision {
	return DocumentRevision{
		ContentHash:          "content-hash-a",
		ExtractionGeneration: CurrentExtractionGeneration,
		FetchedAt:            time.Date(2026, 10, 1, 8, 0, 0, 123456789, time.UTC),
	}
}

func TestDocumentRevisionAcceptsCompleteRevision(t *testing.T) {
	revision := storedDocumentRevision()
	revision.ContentHash = strings.Repeat("h", MaximumDocumentMetadataBytes)
	if err := revision.Validate(); err != nil {
		t.Fatalf("Validate(complete revision) = %v", err)
	}
}

func TestDocumentRevisionRefusesAbsentStoredDocument(t *testing.T) {
	incomplete := map[string]func(*DocumentRevision){
		"no document":     func(revision *DocumentRevision) { *revision = DocumentRevision{} },
		"no content hash": func(revision *DocumentRevision) { revision.ContentHash = "" },
		"no generation":   func(revision *DocumentRevision) { revision.ExtractionGeneration = 0 },
		"no fetch time":   func(revision *DocumentRevision) { revision.FetchedAt = time.Time{} },
		"decoded no fetch time": func(revision *DocumentRevision) {
			revision.FetchedAt = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
		},
	}
	for name, change := range incomplete {
		revision := storedDocumentRevision()
		change(&revision)
		if err := revision.Validate(); !errors.Is(err, errDocumentRevisionIncomplete) {
			t.Errorf("%s: Validate(%+v) = %v, want %v",
				name, revision, err, errDocumentRevisionIncomplete)
		}
	}
}

func TestDocumentRevisionRefusesOversizedContentHash(t *testing.T) {
	revision := storedDocumentRevision()
	revision.ContentHash = strings.Repeat("h", MaximumDocumentMetadataBytes+1)
	if err := revision.Validate(); !errors.Is(err, errDocumentRevisionTooLarge) {
		t.Fatalf("Validate(oversized content hash) = %v, want %v", err, errDocumentRevisionTooLarge)
	}
}

func TestDocumentRevisionMatchesOnlyTheSameStoredDocument(t *testing.T) {
	issued := storedDocumentRevision()
	sameInstant := issued
	sameInstant.FetchedAt = issued.FetchedAt.In(time.FixedZone("UTC+2", 2*60*60))
	if !issued.Matches(sameInstant) {
		t.Fatal("revision with the same fetch instant in another zone did not match")
	}
	stale := map[string]func(*DocumentRevision){
		"changed content": func(revision *DocumentRevision) { revision.ContentHash = "content-hash-b" },
		"re-extracted":    func(revision *DocumentRevision) { revision.ExtractionGeneration++ },
		"refetched": func(revision *DocumentRevision) {
			revision.FetchedAt = revision.FetchedAt.Add(time.Nanosecond)
		},
		"removed": func(revision *DocumentRevision) { *revision = DocumentRevision{} },
	}
	for name, change := range stale {
		current := storedDocumentRevision()
		change(&current)
		if issued.Matches(current) {
			t.Errorf("%s: issued revision matched current revision %+v", name, current)
		}
	}
}
