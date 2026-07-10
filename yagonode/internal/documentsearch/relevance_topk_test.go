package documentsearch

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func relDoc(id string, occurrences, spread uint64) matchedDocument {
	return matchedDocument{
		identifier:  hashFor(id),
		occurrences: occurrences,
		maxPosition: spread,
	}
}

func relDocs(docs ...matchedDocument) map[yagomodel.Hash]matchedDocument {
	set := make(map[yagomodel.Hash]matchedDocument, len(docs))
	for _, doc := range docs {
		set[doc.identifier] = doc
	}

	return set
}

// TestMostRelevantDocumentsEqualsFullSort is the differential oracle: the bounded
// top-k selection must return exactly what ranking the whole set and truncating
// returns, for every limit — the unbounded fallback, the no-truncation fallback,
// and bounded limits that split ties (equal occurrences, equal term spread) across
// the k boundary so the identifier tie-break decides.
func TestMostRelevantDocumentsEqualsFullSort(t *testing.T) {
	docs := relDocs(
		relDoc("a", 5, 0), // highest occurrences
		relDoc("b", 3, 1), // occurrences tie, spread 1
		relDoc("c", 3, 1), // ties b on occurrences and spread -> identifier breaks
		relDoc("d", 3, 5), // occurrences tie, larger spread
		relDoc("e", 1, 0), // occurrences tie with f
		relDoc("f", 1, 0), // ties e on occurrences and spread -> identifier breaks
	)
	for _, limit := range []int{-1, 0, 1, 2, 3, 4, 5, 6, 7} {
		want := takeMostRelevant(documentsOrderedByRelevance(docs), limit)
		got := mostRelevantDocuments(docs, limit)
		if !slices.Equal(got, want) {
			t.Fatalf("limit %d: got %v, want %v", limit, got, want)
		}
	}
}

func TestMostRelevantDocumentsEmpty(t *testing.T) {
	if got := mostRelevantDocuments(nil, 5); len(got) != 0 {
		t.Fatalf("empty set = %v, want none", got)
	}
}

// TestMostRelevantDocumentsSingleEviction covers the smallest bounded path: one
// document over the limit forces exactly one heap eviction of the least relevant.
func TestMostRelevantDocumentsSingleEviction(t *testing.T) {
	docs := relDocs(relDoc("a", 3, 0), relDoc("b", 2, 0))
	got := mostRelevantDocuments(docs, 1)
	if len(got) != 1 || got[0] != hashFor("a") {
		t.Fatalf("got %v, want only the more relevant a", got)
	}
}
