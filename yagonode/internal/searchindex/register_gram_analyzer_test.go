package searchindex

import (
	"errors"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

func TestNewSearchIndexMappingPropagatesGramRegisterError(t *testing.T) {
	old := registerGramAnalyzer
	t.Cleanup(func() { registerGramAnalyzer = old })
	sentinel := errors.New("gram register failed")
	registerGramAnalyzer = func(*mapping.IndexMappingImpl) error { return sentinel }

	if _, err := newSearchIndexMapping(); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestRegisterGramAnalyzerRejectsDuplicateNormalizer(t *testing.T) {
	m := bleve.NewIndexMapping()
	if err := m.AddCustomTokenFilter(unicodeNormFilter, map[string]any{
		"type": "normalize_unicode",
		"form": "nfkc",
	}); err != nil {
		t.Fatalf("seed normalizer: %v", err)
	}

	if err := registerGramAnalyzer(m); err == nil {
		t.Fatal("expected a duplicate-normalizer registration error")
	}
}

func TestRegisterGramAnalyzerRejectsDuplicateTrigramFilter(t *testing.T) {
	m := bleve.NewIndexMapping()
	if err := m.AddCustomTokenFilter(gramTokenFilter, map[string]any{
		"type": "ngram",
		"min":  gramSize,
		"max":  gramSize,
	}); err != nil {
		t.Fatalf("seed trigram filter: %v", err)
	}

	if err := registerGramAnalyzer(m); err == nil {
		t.Fatal("expected a duplicate-trigram-filter registration error")
	}
}

func TestRegisterGramAnalyzerRejectsDuplicateAnalyzer(t *testing.T) {
	m := bleve.NewIndexMapping()
	if err := m.AddCustomAnalyzer(searchGramAnalyzer, map[string]any{
		"type":          "custom",
		"tokenizer":     "unicode",
		"token_filters": []string{lowercaseFilter},
	}); err != nil {
		t.Fatalf("seed analyzer: %v", err)
	}

	if err := registerGramAnalyzer(m); err == nil {
		t.Fatal("expected a duplicate-gram-analyzer registration error")
	}
}
