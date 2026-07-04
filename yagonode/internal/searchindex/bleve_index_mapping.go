package searchindex

import (
	"github.com/blevesearch/bleve/v2"
	_ "github.com/blevesearch/bleve/v2/analysis/analyzer/simple"
	"github.com/blevesearch/bleve/v2/mapping"
)

const (
	searchTextAnalyzer = "standard"
	searchURLAnalyzer  = "simple"
)

func searchIndexedFields() []string {
	return []string{"title", "headings", "anchors", "body", "url"}
}

func searchFieldAnalyzer(field string) string {
	if field == "url" {
		return searchURLAnalyzer
	}

	return searchTextAnalyzer
}

func newSearchIndexMapping() *mapping.IndexMappingImpl {
	indexMapping := bleve.NewIndexMapping()

	document := bleve.NewDocumentMapping()
	document.Dynamic = false
	for _, field := range searchIndexedFields() {
		document.AddFieldMappingsAt(field, newSearchTextField(searchFieldAnalyzer(field)))
	}

	indexMapping.DefaultMapping = document
	indexMapping.StoreDynamic = false
	indexMapping.IndexDynamic = false
	indexMapping.DocValuesDynamic = false

	return indexMapping
}

func newSearchTextField(analyzer string) *mapping.FieldMapping {
	field := bleve.NewTextFieldMapping()
	field.Analyzer = analyzer
	field.Store = false
	field.IncludeInAll = false
	field.IncludeTermVectors = false
	field.DocValues = false

	return field
}
