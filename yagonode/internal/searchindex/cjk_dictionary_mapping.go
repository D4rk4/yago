package searchindex

import (
	"fmt"

	"github.com/blevesearch/bleve/v2/mapping"
)

type cjkAnalyzerRegistration struct {
	analyzer   string
	tokenizer  string
	language   string
	dictionary bool
}

var registerCJKDictionaryAnalyzers = func(indexMapping *mapping.IndexMappingImpl) error {
	if errCJKTokenizerRegistration != nil {
		return fmt.Errorf("register CJK tokenizer type: %w", errCJKTokenizerRegistration)
	}
	registrations := []cjkAnalyzerRegistration{
		{cjkChineseTextAnalyzer, cjkChineseComponent, "zh", true},
		{cjkJapaneseTextAnalyzer, cjkJapaneseComponent, "ja", true},
		{cjkKoreanTextAnalyzer, cjkKoreanComponent, "ko", false},
		{cjkChineseQueryAnalyzer, cjkChineseQueryComponent, "zh", false},
		{cjkJapaneseQueryAnalyzer, cjkJapaneseQueryComponent, "ja", false},
		{cjkKoreanQueryAnalyzer, cjkKoreanQueryComponent, "ko", false},
	}
	for _, registration := range registrations {
		if err := indexMapping.AddCustomTokenizer(registration.tokenizer, map[string]any{
			"type":       cjkDictionaryComponent,
			"language":   registration.language,
			"dictionary": registration.dictionary,
		}); err != nil {
			return fmt.Errorf("register %s tokenizer: %w", registration.language, err)
		}
		if err := indexMapping.AddCustomAnalyzer(registration.analyzer, map[string]any{
			"type":          "custom",
			"tokenizer":     registration.tokenizer,
			"token_filters": []string{lowercaseFilter, unicodeNormFilter},
		}); err != nil {
			return fmt.Errorf("register %s analyzer: %w", registration.language, err)
		}
	}

	return nil
}

func supportsCJKDictionaryAnalyzers(indexMapping mapping.IndexMapping) bool {
	if indexMapping == nil {
		return false
	}
	for _, name := range []string{
		cjkChineseTextAnalyzer,
		cjkJapaneseTextAnalyzer,
		cjkKoreanTextAnalyzer,
		cjkChineseQueryAnalyzer,
		cjkJapaneseQueryAnalyzer,
		cjkKoreanQueryAnalyzer,
	} {
		if indexMapping.AnalyzerNamed(name) == nil {
			return false
		}
	}

	return true
}
