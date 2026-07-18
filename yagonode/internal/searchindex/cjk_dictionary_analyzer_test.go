package searchindex

import (
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2/analysis"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestCJKDocumentAnalyzersEmitRecallAndDictionaryTerms(t *testing.T) {
	tests := []struct {
		name           string
		analyzer       string
		text           string
		unigram        string
		bigram         string
		dictionaryTerm string
	}{
		{
			name:           "Chinese",
			analyzer:       cjkChineseTextAnalyzer,
			text:           "前搜索引擎後",
			unigram:        "搜",
			bigram:         "搜索",
			dictionaryTerm: "搜索引擎",
		},
		{
			name:           "Japanese",
			analyzer:       cjkJapaneseTextAnalyzer,
			text:           "プログラミング言語",
			unigram:        "プ",
			bigram:         "プロ",
			dictionaryTerm: "プログラミング",
		},
		{
			name:     "Korean",
			analyzer: cjkKoreanTextAnalyzer,
			text:     "검색엔진",
			unigram:  "검",
			bigram:   "검색",
		},
	}
	mapping := loadStemmingMapping()
	if mapping == nil {
		t.Fatal("stemming mapping unavailable")
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokens := mapping.AnalyzerNamed(test.analyzer).Analyze([]byte(test.text))
			assertCJKToken(t, test.text, tokens, test.unigram, analysis.Single)
			assertCJKToken(t, test.text, tokens, test.bigram, analysis.Double)
			if test.dictionaryTerm != "" {
				assertCJKToken(
					t,
					test.text,
					tokens,
					test.dictionaryTerm,
					analysis.Shingle,
				)
			}
		})
	}
}

func TestCJKQueryAnalyzersExcludeDictionaryTerms(t *testing.T) {
	mapping := loadStemmingMapping()
	for _, analyzer := range []string{
		cjkChineseQueryAnalyzer,
		cjkJapaneseQueryAnalyzer,
		cjkKoreanQueryAnalyzer,
	} {
		for _, token := range mapping.AnalyzerNamed(analyzer).Analyze([]byte("搜索引擎")) {
			if token.Type == analysis.Shingle {
				t.Fatalf("%s emitted dictionary token %#v", analyzer, token)
			}
		}
	}
}

func TestCJKChineseAnalyzerCanonicalizesTraditionalTextAtOriginalOffsets(t *testing.T) {
	text := "甲搜尋軟體乙"
	tokens := loadStemmingMapping().AnalyzerNamed(cjkChineseTextAnalyzer).Analyze([]byte(text))
	for _, term := range []string{"搜索", "软件"} {
		token := findCJKToken(tokens, term, analysis.Double)
		if token == nil {
			t.Fatalf("canonical token %q missing from %v", term, cjkTokenTerms(tokens))
		}
		if token.Start < 0 || token.End > len(text) ||
			len([]rune(text[token.Start:token.End])) != 2 {
			t.Fatalf("canonical token offsets = %#v in %q", token, text)
		}
	}
}

func cjkTokenTerms(tokens analysis.TokenStream) []string {
	terms := make([]string, 0, len(tokens))
	for _, token := range tokens {
		terms = append(terms, string(token.Term))
	}

	return terms
}

func TestCJKRecallDoesNotDependOnLongestDictionarySegment(t *testing.T) {
	tests := []struct {
		name     string
		language string
		document string
		query    string
	}{
		{
			name:     "Chinese larger word",
			language: "zh-Hans",
			document: "程序设计语言",
			query:    "程序设计",
		},
		{
			name:     "Japanese larger word",
			language: "ja",
			document: "プログラミング言語",
			query:    "プログラミング",
		},
		{
			name:     "Chinese unigram",
			language: "zh-Hans",
			document: "网络技术",
			query:    "网",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			index, err := NewBleveMemoryIndex(t.Context(), nil)
			if err != nil {
				t.Fatalf("NewBleveMemoryIndex: %v", err)
			}
			if err := index.Index(t.Context(), documentstore.Document{
				NormalizedURL: "https://example.test/" + strings.ReplaceAll(test.name, " ", "-"),
				ExtractedText: test.document,
				Language:      test.language,
			}); err != nil {
				t.Fatalf("Index: %v", err)
			}
			result, err := index.Search(t.Context(), SearchRequest{
				Query:      test.query,
				Terms:      []string{test.query},
				MaxResults: 1,
			})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if result.Total != 1 || len(result.Results) != 1 {
				t.Fatalf("recall = %#v", result)
			}
		})
	}
}

func TestCJKChineseSearchCanonicalizesBothScriptDirections(t *testing.T) {
	tests := []struct {
		document string
		query    string
	}{
		{document: "搜索软件", query: "搜尋軟體"},
		{document: "搜尋軟體", query: "搜索软件"},
	}
	for _, test := range tests {
		index, err := NewBleveMemoryIndex(t.Context(), nil)
		if err != nil {
			t.Fatalf("NewBleveMemoryIndex: %v", err)
		}
		if err := index.Index(t.Context(), documentstore.Document{
			NormalizedURL: "https://example.test/chinese-script",
			ExtractedText: test.document,
			Language:      "zh",
		}); err != nil {
			t.Fatalf("Index: %v", err)
		}
		result, err := index.Search(t.Context(), SearchRequest{
			Query:      test.query,
			Terms:      []string{test.query},
			MaxResults: 1,
		})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if result.Total != 1 {
			t.Fatalf("script-equivalent recall = %#v", result)
		}
	}
}

func assertCJKToken(
	t *testing.T,
	text string,
	tokens analysis.TokenStream,
	term string,
	tokenType analysis.TokenType,
) {
	t.Helper()
	token := findCJKToken(tokens, term, tokenType)
	if token == nil {
		t.Fatalf("term %q type %v missing from %#v", term, tokenType, tokens)
	}
	start := strings.Index(text, term)
	if start >= 0 && (token.Start != start || token.End != start+len(term)) {
		t.Fatalf(
			"term %q offsets = %d:%d, want %d:%d",
			term,
			token.Start,
			token.End,
			start,
			start+len(term),
		)
	}
}

func findCJKToken(
	tokens analysis.TokenStream,
	term string,
	tokenType analysis.TokenType,
) *analysis.Token {
	for _, token := range tokens {
		if string(token.Term) == term && token.Type == tokenType {
			return token
		}
	}

	return nil
}
