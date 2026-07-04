package searchindex

import (
	"fmt"

	"github.com/blevesearch/bleve/v2"
	_ "github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/en"
	_ "github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	_ "github.com/blevesearch/bleve/v2/analysis/tokenizer/regexp"
	"github.com/blevesearch/bleve/v2/mapping"
)

const (
	// searchTextAnalyzer stems and stop-filters English web text so queries like
	// "running" match indexed "run"; both indexing and querying use it, so the
	// stemming stays consistent and non-English tokens pass through unchanged
	// rather than being mis-stemmed. Per-language stemming is deferred: Bleve
	// analyzes a query with a single field analyzer, so correct multilingual
	// stemming needs per-language sub-fields, a separate change.
	searchTextAnalyzer = "en"
	// searchURLAnalyzer splits URLs and host labels on their punctuation while
	// keeping digits (web3, 2024, h2). The unicode tokenizer keeps a host like
	// "a.example.net" whole, and the letter-only simple analyzer drops digits, so
	// a custom tokenizer is registered instead. Its regexp matches runs of any
	// script's letters, digits, and combining marks, so internationalized domains
	// and non-ASCII paths (Cyrillic, CJK, Arabic, ...) tokenize too.
	searchURLAnalyzer = "weburl"
	urlWordSplitter   = "weburl_word"
	lowercaseFilter   = "to_lower"
	urlWordRegexp     = `[\p{L}\p{N}\p{M}]+`
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

func newSearchIndexMapping() (*mapping.IndexMappingImpl, error) {
	indexMapping := bleve.NewIndexMapping()
	if err := registerURLAnalyzer(indexMapping); err != nil {
		return nil, err
	}

	document := bleve.NewDocumentMapping()
	document.Dynamic = false
	for _, field := range searchIndexedFields() {
		document.AddFieldMappingsAt(field, newSearchTextField(searchFieldAnalyzer(field)))
	}

	indexMapping.DefaultMapping = document
	indexMapping.StoreDynamic = false
	indexMapping.IndexDynamic = false
	indexMapping.DocValuesDynamic = false

	return indexMapping, nil
}

// registerURLAnalyzer wires an alphanumeric-run tokenizer plus lowercasing so the
// url field tokenizes host labels and path segments while keeping their digits.
func registerURLAnalyzer(indexMapping *mapping.IndexMappingImpl) error {
	if err := indexMapping.AddCustomTokenizer(urlWordSplitter, map[string]any{
		"type":   "regexp",
		"regexp": urlWordRegexp,
	}); err != nil {
		return fmt.Errorf("register url tokenizer: %w", err)
	}
	if err := indexMapping.AddCustomAnalyzer(searchURLAnalyzer, map[string]any{
		"type":          "custom",
		"tokenizer":     urlWordSplitter,
		"token_filters": []string{lowercaseFilter},
	}); err != nil {
		return fmt.Errorf("register url analyzer: %w", err)
	}

	return nil
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
