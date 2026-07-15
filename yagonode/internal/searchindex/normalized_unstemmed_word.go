package searchindex

import (
	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/analysis/lang/cjk"
	"github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	"github.com/blevesearch/bleve/v2/analysis/token/unicodenorm"
)

var (
	cjkUnstemmedNormalizer       = cjk.NewCJKWidthFilter()
	unstemmedLowercaseNormalizer = lowercase.NewLowerCaseFilter()
	standardUnstemmedNormalizer  = unicodenorm.MustNewUnicodeNormalizeFilter(unicodenorm.NFKC)
)

func normalizedUnstemmedWord(word string, analyzerName string) string {
	token := &analysis.Token{Term: []byte(word)}
	tokens := analysis.TokenStream{token}
	if analyzerName == "cjk" {
		tokens = cjkUnstemmedNormalizer.Filter(tokens)
		tokens = unstemmedLowercaseNormalizer.Filter(tokens)
	} else {
		tokens = unstemmedLowercaseNormalizer.Filter(tokens)
		tokens = standardUnstemmedNormalizer.Filter(tokens)
	}

	return string(tokens[0].Term)
}
