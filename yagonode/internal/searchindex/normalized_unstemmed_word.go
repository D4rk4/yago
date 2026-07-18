package searchindex

import (
	"strings"

	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/analysis/lang/cjk"
	"github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	"github.com/blevesearch/bleve/v2/analysis/token/unicodenorm"
	bleveunicode "github.com/blevesearch/bleve/v2/analysis/tokenizer/unicode"
)

var (
	cjkUnstemmedNormalizer       = cjk.NewCJKWidthFilter()
	unstemmedLowercaseNormalizer = lowercase.NewLowerCaseFilter()
	standardUnstemmedNormalizer  = unicodenorm.MustNewUnicodeNormalizeFilter(unicodenorm.NFKC)
)

func normalizedUnstemmedWord(word string, analyzerName string) string {
	if isCJKAnalyzer(analyzerName) && analyzerName != "cjk" {
		return normalizedCJKSurface(word, analyzerName)
	}
	token := &analysis.Token{Term: []byte(word)}
	tokens := analysis.TokenStream{token}
	if isCJKAnalyzer(analyzerName) {
		tokens = cjkUnstemmedNormalizer.Filter(tokens)
		tokens = unstemmedLowercaseNormalizer.Filter(tokens)
	} else {
		tokens = unstemmedLowercaseNormalizer.Filter(tokens)
		tokens = standardUnstemmedNormalizer.Filter(tokens)
	}

	return string(tokens[0].Term)
}

func normalizedCJKSurface(word string, analyzerName string) string {
	language := "ko"
	switch analyzerName {
	case cjkChineseTextAnalyzer, cjkChineseQueryAnalyzer:
		language = "zh"
	case cjkJapaneseTextAnalyzer, cjkJapaneseQueryAnalyzer:
		language = "ja"
	}
	configuration, _ := cjkTokenizerConfiguration(language, false)
	tokenizer := &cjkDictionaryTokenizerInstance{
		base:           bleveunicode.NewUnicodeTokenizer(),
		canonicalize:   configuration.canonicalize,
		splitHangul:    configuration.splitHangul,
		widthNormalize: cjk.NewCJKWidthFilter(),
	}
	tokens := tokenizer.Tokenize([]byte(word))
	tokens = unstemmedLowercaseNormalizer.Filter(tokens)
	tokens = standardUnstemmedNormalizer.Filter(tokens)
	out := strings.Builder{}
	for _, token := range tokens {
		if token.Type != analysis.Double && token.Type != analysis.Shingle {
			out.Write(token.Term)
		}
	}

	return out.String()
}
