package searchindex

import (
	"fmt"

	"github.com/blevesearch/bleve/v2"
	_ "github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/en"
	_ "github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	"github.com/blevesearch/bleve/v2/analysis/token/ngram"
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
	// searchGramAnalyzer indexes overlapping character trigrams so a query finds a
	// morphological variant or truncation of an indexed word ("зеленски" ->
	// "зеленский") in ANY language, with no per-language stemmer. The unicode
	// tokenizer + lowercasing + NFKC normalization make the grams script-agnostic;
	// trigrams (McNamee & Mayfield 2004: n=3-4 substitutes for stemming, gains
	// correlate with morphological richness). It supplements, never replaces, the
	// exact fields: gram matches are OR'd into the main disjunction at a lower
	// boost so full-word matches keep ranking above shared-gram matches. Follow-up:
	// an edge-ngram prefix field, query-time fuzzy, and an OR-recall variant.
	searchGramAnalyzer = "text_gram"
	gramTokenFilter    = "trigram"
	unicodeNormFilter  = "normalize_nfkc"
	gramFieldSuffix    = "_gram"
	gramSize           = 3.0
	// gramWeightFactor scales a field's ranking weight for its trigram clause so a
	// document matching all query trigrams (AND) ranks below an exact word match.
	gramWeightFactor = 0.5
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

// newSearchIndexMapping is a package var so a test can inject a mapping-build
// failure and exercise the callers' error handling.
var newSearchIndexMapping = func() (*mapping.IndexMappingImpl, error) {
	indexMapping := bleve.NewIndexMapping()
	if err := registerURLAnalyzer(indexMapping); err != nil {
		return nil, err
	}
	if err := registerGramAnalyzer(indexMapping); err != nil {
		return nil, err
	}

	document := bleve.NewDocumentMapping()
	document.Dynamic = false
	for _, field := range searchIndexedFields() {
		fields := []*mapping.FieldMapping{newSearchTextField(searchFieldAnalyzer(field))}
		if fieldSupportsGrams(field) {
			fields = append(fields, newSearchGramField(field+gramFieldSuffix))
		}
		document.AddFieldMappingsAt(field, fields...)
	}

	indexMapping.DefaultMapping = document
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

// fieldSupportsGrams reports whether a source field also gets a trigram sub-field.
// The url field keeps only its punctuation-splitting analyzer; trigrams over host
// labels and path segments add noise without helping word-level recall.
func fieldSupportsGrams(field string) bool {
	return field != "url"
}

// newSearchGramField maps a source field a second time under a "<field>_gram"
// name analyzed into character trigrams, so the same document text is searchable
// both as whole words (precision) and as language-agnostic grams (recall).
func newSearchGramField(name string) *mapping.FieldMapping {
	field := newSearchTextField(searchGramAnalyzer)
	field.Name = name

	return field
}

// supportsGramAnalyzer reports whether the index's mapping can resolve the
// trigram analyzer. An index created before the analyzer existed keeps its
// original persisted mapping for life, and a query that references the
// analyzer against such an index fails the whole search with
// "no analyzer named 'text_gram' registered".
func supportsGramAnalyzer(index bleve.Index) bool {
	return index.Mapping().AnalyzerNamed(searchGramAnalyzer) != nil
}

// registerGramAnalyzer wires the language-agnostic trigram analyzer: the unicode
// tokenizer splits any script into words, lowercasing and NFKC normalization fold
// case and width/compatibility variants, and the ngram filter emits overlapping
// character trigrams. It is a package var so a test can force a registration
// failure. See searchGramAnalyzer for the rationale.
var registerGramAnalyzer = func(indexMapping *mapping.IndexMappingImpl) error {
	if err := indexMapping.AddCustomTokenFilter(unicodeNormFilter, map[string]any{
		"type": unicodenorm.Name,
		"form": unicodenorm.NFKC,
	}); err != nil {
		return fmt.Errorf("register unicode normaliser: %w", err)
	}
	if err := indexMapping.AddCustomTokenFilter(gramTokenFilter, map[string]any{
		"type": ngram.Name,
		"min":  gramSize,
		"max":  gramSize,
	}); err != nil {
		return fmt.Errorf("register trigram filter: %w", err)
	}
	if err := indexMapping.AddCustomAnalyzer(searchGramAnalyzer, map[string]any{
		"type":          "custom",
		"tokenizer":     "unicode",
		"token_filters": []string{lowercaseFilter, unicodeNormFilter, gramTokenFilter},
	}); err != nil {
		return fmt.Errorf("register gram analyzer: %w", err)
	}

	return nil
}
