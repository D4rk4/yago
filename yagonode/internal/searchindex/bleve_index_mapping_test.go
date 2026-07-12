package searchindex

import (
	"testing"

	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type nilMappingIndex struct {
	bleveIndexContract
}

func (nilMappingIndex) Mapping() mapping.IndexMapping {
	return nil
}

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
	analyzer := indexMapping.DefaultMapping.Properties[documentAnalyzerField]
	if analyzer == nil || len(analyzer.Fields) != 1 ||
		analyzer.Fields[0].Analyzer != "keyword" ||
		!analyzer.Fields[0].Index || !analyzer.Fields[0].Store {
		t.Fatalf("analyzer scope mapping = %#v", analyzer)
	}
	candidate := indexMapping.DefaultMapping.Properties[storedCandidateField]
	if candidate == nil || len(candidate.Fields) != 1 || candidate.Fields[0].Index ||
		!candidate.Fields[0].Store || candidate.Fields[0].IncludeTermVectors ||
		candidate.Fields[0].DocValues {
		t.Fatalf("stored candidate mapping = %#v", candidate)
	}

	if host := indexMapping.DefaultMapping.Properties["host"]; host != nil {
		t.Fatalf("host field should not be mapped, got %#v", host)
	}
}

func TestStoredCandidateProjectionRejectsAbsentMapping(t *testing.T) {
	if supportsStoredCandidateProjection(nilMappingIndex{}) ||
		supportsAnalyzerScope(nilMappingIndex{}) {
		t.Fatal("nil mapping supports current fields")
	}
}

// defaultMappingAnalyzer is the analyzer the fallback (standard) document
// mapping applies to a field: the punctuation splitter for the url field, the
// no-stemming standard analyzer for text.
func defaultMappingAnalyzer(field string) string {
	if field == "url" {
		return searchURLAnalyzer
	}

	return standardTextAnalyzer
}

func assertTunedSearchField(t *testing.T, field string, document *mapping.DocumentMapping) {
	t.Helper()

	if document == nil || len(document.Fields) != 1 {
		t.Fatalf("field %q mapping = %#v", field, document)
	}

	exact := document.Fields[0]
	if !exact.Index || exact.IncludeTermVectors {
		t.Fatalf("field %q indexing options = %#v", field, exact)
	}
	if exact.Store || exact.IncludeInAll || exact.DocValues {
		t.Fatalf("field %q exact sub-field not tuned down: %#v", field, exact)
	}
	if exact.Analyzer != defaultMappingAnalyzer(field) {
		t.Fatalf(
			"field %q analyzer = %q, want %q",
			field,
			exact.Analyzer,
			defaultMappingAnalyzer(field),
		)
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

func TestFuzzySearchMatchesTruncatedWord(t *testing.T) {
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

	results, err := index.Search(
		t.Context(),
		SearchRequest{Query: "зеленски", MaxResults: 5, Fuzzy: true},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 {
		t.Fatalf(`results = %#v, want truncated "зеленски" to match indexed "Зеленский"`, results)
	}
}

func TestOrdinaryQueriesRejectScatteredPartialWords(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{
			{
				NormalizedURL: "https://anticisco.ru/blog",
				Title:         "antiCisco blogs cisco",
				ExtractedText: "через черно многие много строгого город которая территория горы",
			},
			{
				NormalizedURL: "https://ru.wikipedia.org/montenegro",
				Title:         "Черногория",
				ExtractedText: "Черногория страна на Балканах столица Подгорица",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	results, err := index.Search(t.Context(), SearchRequest{Query: "черногория", MaxResults: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 || results.Results[0].URL != "https://ru.wikipedia.org/montenegro" {
		t.Fatalf("ordinary query flooded by scattered trigrams: %#v", results)
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
