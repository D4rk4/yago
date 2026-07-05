package searchindex

import (
	"testing"

	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestNewSearchIndexMappingTunesFields(t *testing.T) {
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		t.Fatalf("newSearchIndexMapping: %v", err)
	}

	if indexMapping.StoreDynamic || indexMapping.IndexDynamic || indexMapping.DocValuesDynamic {
		t.Fatalf("dynamic flags store=%v index=%v docvalues=%v, want all false",
			indexMapping.StoreDynamic, indexMapping.IndexDynamic, indexMapping.DocValuesDynamic)
	}
	if indexMapping.DefaultMapping.Dynamic {
		t.Fatal("default document mapping is dynamic, want static")
	}

	for _, field := range searchIndexedFields() {
		assertTunedSearchField(t, field, indexMapping.DefaultMapping.Properties[field])
	}

	if host := indexMapping.DefaultMapping.Properties["host"]; host != nil {
		t.Fatalf("host field should not be mapped, got %#v", host)
	}
}

// assertTunedSearchField checks a source field maps to a tuned-down exact word
// field (correct analyzer, no store/term-vectors/doc-values) plus, for grammed
// fields, a trigram sub-field named "<field>_gram" with the gram analyzer.
func assertTunedSearchField(t *testing.T, field string, document *mapping.DocumentMapping) {
	t.Helper()

	wantFields := 1
	if fieldSupportsGrams(field) {
		wantFields = 2 // exact word field plus its trigram sub-field
	}
	if document == nil || len(document.Fields) != wantFields {
		t.Fatalf("field %q mapping = %#v", field, document)
	}

	exact := document.Fields[0]
	if !exact.Index {
		t.Fatalf("field %q is not indexed", field)
	}
	if exact.Store || exact.IncludeInAll || exact.IncludeTermVectors || exact.DocValues {
		t.Fatalf("field %q exact sub-field not tuned down: %#v", field, exact)
	}
	if exact.Analyzer != searchFieldAnalyzer(field) {
		t.Fatalf(
			"field %q analyzer = %q, want %q",
			field,
			exact.Analyzer,
			searchFieldAnalyzer(field),
		)
	}

	if !fieldSupportsGrams(field) {
		return
	}
	gram := document.Fields[1]
	if gram.Name != field+gramFieldSuffix || gram.Analyzer != searchGramAnalyzer {
		t.Fatalf("field %q gram sub-field = %#v", field, gram)
	}
	if gram.Store || gram.IncludeInAll || gram.IncludeTermVectors || gram.DocValues {
		t.Fatalf("field %q gram sub-field not tuned down: %#v", field, gram)
	}
}

func TestSearchMatchesHostKeywordInURL(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://searchengine.example.net/page",
			Title:         "Unrelated",
			ExtractedText: "Body without the host word.",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	results, err := index.Search(t.Context(), SearchRequest{Query: "searchengine", MaxResults: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 || len(results.Results) != 1 {
		t.Fatalf(
			"results = %#v, want the host keyword to match through the tuned url analyzer",
			results,
		)
	}
}

func TestSearchStemsEnglishText(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://example.net/guide",
			Title:         "Guide",
			ExtractedText: "Developers enjoy running marathons.",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	results, err := index.Search(t.Context(), SearchRequest{Query: "run", MaxResults: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 {
		t.Fatalf(`results = %#v, want stemmed "run" to match indexed "running"`, results)
	}
}

func TestSearchMatchesDigitInURL(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://example.net/reports/2024/q1",
			Title:         "Report",
			ExtractedText: "Body without the year.",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	results, err := index.Search(t.Context(), SearchRequest{Query: "2024", MaxResults: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 {
		t.Fatalf("results = %#v, want the URL year digit to tokenize and match", results)
	}
}

func TestSearchMatchesTruncatedWordViaTrigrams(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://example.net/news",
			Title:         "Новость",
			ExtractedText: "Президент Зеленский выступил с заявлением.",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	// A truncated query ("зеленски" for indexed "Зеленский") shares every trigram
	// of the query, so the language-agnostic gram field matches with no Russian
	// stemmer configured — the exact word field alone would return nothing.
	results, err := index.Search(t.Context(), SearchRequest{Query: "зеленски", MaxResults: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 {
		t.Fatalf(`results = %#v, want truncated "зеленски" to match indexed "Зеленский"`, results)
	}
}

func TestSearchMatchesUnicodeHostLabel(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://поиск.example.net/страница",
			Title:         "Unrelated",
			ExtractedText: "Body without the host word.",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	// An ASCII-only tokenizer would drop the Cyrillic labels; the Unicode regexp
	// keeps them, so internationalized hosts and paths stay searchable.
	results, err := index.Search(t.Context(), SearchRequest{Query: "поиск", MaxResults: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 {
		t.Fatalf("results = %#v, want the Cyrillic host label to tokenize and match", results)
	}
}
