package searchindex

import (
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/documentstore"
)

func TestNewSearchIndexMappingTunesFields(t *testing.T) {
	indexMapping := newSearchIndexMapping()

	if indexMapping.StoreDynamic || indexMapping.IndexDynamic || indexMapping.DocValuesDynamic {
		t.Fatalf("dynamic flags store=%v index=%v docvalues=%v, want all false",
			indexMapping.StoreDynamic, indexMapping.IndexDynamic, indexMapping.DocValuesDynamic)
	}
	if indexMapping.DefaultMapping.Dynamic {
		t.Fatal("default document mapping is dynamic, want static")
	}

	for _, field := range searchIndexedFields() {
		document := indexMapping.DefaultMapping.Properties[field]
		if document == nil || len(document.Fields) != 1 {
			t.Fatalf("field %q mapping = %#v", field, document)
		}
		fieldMapping := document.Fields[0]
		if !fieldMapping.Index {
			t.Fatalf("field %q is not indexed", field)
		}
		if fieldMapping.Store || fieldMapping.IncludeInAll ||
			fieldMapping.IncludeTermVectors || fieldMapping.DocValues {
			t.Fatalf(
				"field %q store=%v includeInAll=%v termVectors=%v docValues=%v, want all false",
				field,
				fieldMapping.Store,
				fieldMapping.IncludeInAll,
				fieldMapping.IncludeTermVectors,
				fieldMapping.DocValues,
			)
		}
		if fieldMapping.Analyzer != searchFieldAnalyzer(field) {
			t.Fatalf(
				"field %q analyzer = %q, want %q",
				field,
				fieldMapping.Analyzer,
				searchFieldAnalyzer(field),
			)
		}
	}

	if host := indexMapping.DefaultMapping.Properties["host"]; host != nil {
		t.Fatalf("host field should not be mapped, got %#v", host)
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
