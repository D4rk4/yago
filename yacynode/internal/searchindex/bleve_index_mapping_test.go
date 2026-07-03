package searchindex

import "testing"

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
			t.Fatalf("field %q store=%v includeInAll=%v termVectors=%v docValues=%v, want all false",
				field, fieldMapping.Store, fieldMapping.IncludeInAll,
				fieldMapping.IncludeTermVectors, fieldMapping.DocValues)
		}
		if fieldMapping.Analyzer != searchTextAnalyzer {
			t.Fatalf("field %q analyzer = %q, want %q", field, fieldMapping.Analyzer, searchTextAnalyzer)
		}
	}

	if host := indexMapping.DefaultMapping.Properties["host"]; host != nil {
		t.Fatalf("host field should not be mapped, got %#v", host)
	}
}
