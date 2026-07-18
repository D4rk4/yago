package searchindex

import (
	"github.com/blevesearch/bleve/v2/analysis"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
)

const cjkDictionaryMatchBoost = 0.2

func cjkDictionaryQueryTerm(analyzer string, text string) bool {
	if !isCJKAnalyzer(analyzer) || analyzer == "cjk" {
		return false
	}
	resolved := storedEvidenceAnalyzer(analyzer)
	if resolved == nil {
		return false
	}
	for _, token := range resolved.Analyze([]byte(text)) {
		if token.Type == analysis.Shingle {
			return true
		}
	}

	return false
}

func fieldCJKDictionaryMatch(
	field string,
	text string,
	boost float64,
	analyzer string,
) *blevequery.MatchQuery {
	query := fieldMatch(field, text, boost*cjkDictionaryMatchBoost)
	query.Analyzer = analyzer

	return query
}
