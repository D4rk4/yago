package searchindex

import (
	"errors"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

func TestNewSearchIndexMappingPropagatesNormalizerRegisterError(t *testing.T) {
	old := registerUnicodeNormalizer
	t.Cleanup(func() { registerUnicodeNormalizer = old })
	sentinel := errors.New("normalizer register failed")
	registerUnicodeNormalizer = func(*mapping.IndexMappingImpl) error { return sentinel }

	if _, err := newSearchIndexMapping(); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestRegisterUnicodeNormalizerRejectsDuplicate(t *testing.T) {
	mapping := bleve.NewIndexMapping()
	if err := mapping.AddCustomTokenFilter(unicodeNormFilter, map[string]any{
		"type": "normalize_unicode",
		"form": "nfkc",
	}); err != nil {
		t.Fatalf("seed normalizer: %v", err)
	}

	if err := registerUnicodeNormalizer(mapping); err == nil {
		t.Fatal("expected a duplicate-normalizer registration error")
	}
}
