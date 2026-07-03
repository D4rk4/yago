package searchindex

import (
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

const searchTextAnalyzer = "standard"

func searchIndexedFields() []string {
	return []string{"title", "headings", "anchors", "body", "url"}
}

func newSearchIndexMapping() *mapping.IndexMappingImpl {
	indexMapping := bleve.NewIndexMapping()

	document := bleve.NewDocumentMapping()
	document.Dynamic = false
	for _, field := range searchIndexedFields() {
		document.AddFieldMappingsAt(field, newSearchTextField())
	}

	indexMapping.DefaultMapping = document
	indexMapping.StoreDynamic = false
	indexMapping.IndexDynamic = false
	indexMapping.DocValuesDynamic = false

	return indexMapping
}

func newSearchTextField() *mapping.FieldMapping {
	field := bleve.NewTextFieldMapping()
	field.Analyzer = searchTextAnalyzer
	field.Store = false
	field.IncludeInAll = false
	field.IncludeTermVectors = false
	field.DocValues = false

	return field
}
