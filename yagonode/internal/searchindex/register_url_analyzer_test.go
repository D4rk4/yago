package searchindex

import (
	"testing"

	"github.com/blevesearch/bleve/v2"
)

func TestRegisterURLAnalyzerRejectsDuplicateTokenizer(t *testing.T) {
	m := bleve.NewIndexMapping()
	if err := m.AddCustomTokenizer(urlWordSplitter, map[string]any{
		"type":   "regexp",
		"regexp": `\w+`,
	}); err != nil {
		t.Fatalf("seed tokenizer: %v", err)
	}

	if err := registerURLAnalyzer(m); err == nil {
		t.Fatal("expected a duplicate-tokenizer registration error")
	}
}

func TestRegisterURLAnalyzerRejectsDuplicateAnalyzer(t *testing.T) {
	m := bleve.NewIndexMapping()
	if err := m.AddCustomTokenizer("seed_tokenizer", map[string]any{
		"type":   "regexp",
		"regexp": `\w+`,
	}); err != nil {
		t.Fatalf("seed tokenizer: %v", err)
	}
	//nolint:gosec // G101 false positive: bleve analyzer config keys, not credentials.
	if err := m.AddCustomAnalyzer(searchURLAnalyzer, map[string]any{
		"type":          "custom",
		"tokenizer":     "seed_tokenizer",
		"token_filters": []string{lowercaseFilter},
	}); err != nil {
		t.Fatalf("seed analyzer: %v", err)
	}

	if err := registerURLAnalyzer(m); err == nil {
		t.Fatal("expected a duplicate-analyzer registration error")
	}
}
