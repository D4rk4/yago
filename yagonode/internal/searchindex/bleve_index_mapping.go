package searchindex

import (
	"fmt"

	"github.com/blevesearch/bleve/v2"
	_ "github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/en"
	_ "github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	"github.com/blevesearch/bleve/v2/analysis/token/unicodenorm"
	_ "github.com/blevesearch/bleve/v2/analysis/tokenizer/regexp"
	_ "github.com/blevesearch/bleve/v2/analysis/tokenizer/unicode"
	"github.com/blevesearch/bleve/v2/mapping"
	bleveindex "github.com/blevesearch/bleve_index_api"
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
	unicodeNormFilter = "normalize_nfkc"
)

func searchIndexedFields() []string {
	return []string{"title", "headings", "anchors", "body", "url"}
}

// documentAnalyzerField is the TypeField whose value (an analyzer name) selects
// the per-language document mapping a document is analyzed with.
const documentAnalyzerField = "_analyzer"

// newSearchIndexMapping is a package var so a test can inject a mapping-build
// failure and exercise the callers' error handling.
var newSearchIndexMapping = func() (*mapping.IndexMappingImpl, error) {
	indexMapping := bleve.NewIndexMapping()
	if err := registerURLAnalyzer(indexMapping); err != nil {
		return nil, err
	}
	if err := registerUnicodeNormalizer(indexMapping); err != nil {
		return nil, err
	}
	if err := registerCJKDictionaryAnalyzers(indexMapping); err != nil {
		return nil, err
	}
	if err := registerStandardTextAnalyzer(indexMapping); err != nil {
		return nil, fmt.Errorf("register standard analyzer: %w", err)
	}

	// Each document is routed by its detected language to a document mapping
	// that analyzes the text fields with that language's stemmer; the standard
	// (no-stemming) mapping is the fallback for unrouted languages.
	indexMapping.TypeField = documentAnalyzerField
	for _, analyzer := range documentAnalyzers() {
		indexMapping.AddDocumentMapping(analyzer, newDocumentMapping(analyzer))
	}
	indexMapping.DefaultMapping = newDocumentMapping(standardTextAnalyzer)
	indexMapping.StoreDynamic = false
	indexMapping.IndexDynamic = false
	indexMapping.DocValuesDynamic = false
	// Okapi BM25 replaces bleve's default TF-IDF: term-frequency saturation
	// keeps a keyword-stuffed page from outranking a concise relevant one, and
	// document-length normalization stops a long page from accumulating score
	// merely by being long (bleve's own scoring guidance; Robertson & Zaragoza,
	// "The Probabilistic Relevance Framework: BM25 and Beyond", 2009).
	indexMapping.ScoringModel = bleveindex.BM25Scoring

	return indexMapping, nil
}

func newDocumentMapping(textAnalyzer string) *mapping.DocumentMapping {
	document := bleve.NewDocumentMapping()
	document.Dynamic = false
	analyzerField := bleve.NewKeywordFieldMapping()
	analyzerField.Store = true
	analyzerField.IncludeInAll = false
	analyzerField.IncludeTermVectors = false
	analyzerField.DocValues = false
	document.AddFieldMappingsAt(documentAnalyzerField, analyzerField)
	candidateField := bleve.NewKeywordFieldMapping()
	candidateField.Index = false
	candidateField.Store = true
	candidateField.IncludeInAll = false
	candidateField.IncludeTermVectors = false
	candidateField.DocValues = false
	document.AddFieldMappingsAt(storedCandidateField, candidateField)
	for _, field := range searchIndexedFields() {
		analyzer := textAnalyzer
		if field == "url" {
			analyzer = searchURLAnalyzer
		}
		document.AddFieldMappingsAt(field, newSearchTextField(analyzer))
	}

	return document
}

// enableBM25Scoring switches an already-opened index to BM25 scoring in place.
// bleve reads the scoring model from the live mapping at search time and a
// model change needs no reindex, so an index persisted under the default
// TF-IDF scoring adopts BM25 the moment it is opened — no rebuild required.
func enableBM25Scoring(index bleve.Index) {
	if mappingImpl, ok := index.Mapping().(*mapping.IndexMappingImpl); ok {
		mappingImpl.ScoringModel = bleveindex.BM25Scoring
	}
}

// registerURLAnalyzer wires an alphanumeric-run tokenizer plus lowercasing so the
// url field tokenizes host labels and path segments while keeping their digits.
// It is a package var so a test can force a mapping-registration failure.
var registerURLAnalyzer = func(indexMapping *mapping.IndexMappingImpl) error {
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

// supportsMultilingualAnalyzers reports whether the index's mapping carries the
// per-language routing added with the standard fallback analyzer. An index
// created before that change cannot resolve the per-language query analyzers,
// so its queries must keep the single default-analyzer clause set.
func supportsMultilingualAnalyzers(index bleve.Index) bool {
	return index.Mapping().AnalyzerNamed(standardTextAnalyzer) != nil
}

func supportsAnalyzerScope(index bleve.Index) bool {
	indexMapping := index.Mapping()
	if indexMapping == nil {
		return false
	}
	field := indexMapping.FieldMappingForPath(documentAnalyzerField)
	if field.Type == "" {
		return false
	}

	return field.Analyzer == "keyword" && field.Index
}

func supportsStoredCandidateProjection(index bleve.Index) bool {
	indexMapping := index.Mapping()
	if indexMapping == nil {
		return false
	}
	field := indexMapping.FieldMappingForPath(storedCandidateField)
	if field.Type == "" {
		return false
	}

	return field.Store && !field.Index && !field.IncludeTermVectors && !field.DocValues
}

func shardMappingIsCurrent(index bleve.Index) bool {
	return supportsMultilingualAnalyzers(index) && supportsAnalyzerScope(index) &&
		supportsStoredCandidateProjection(index) &&
		supportsCJKDictionaryAnalyzers(index.Mapping())
}

var registerUnicodeNormalizer = func(indexMapping *mapping.IndexMappingImpl) error {
	return indexMapping.AddCustomTokenFilter(unicodeNormFilter, map[string]any{
		"type": unicodenorm.Name,
		"form": unicodenorm.NFKC,
	})
}
