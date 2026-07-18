package searchindex

import (
	"strconv"
	"strings"

	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
)

func requirementAnalyzerBranches(
	req SearchRequest,
	analyzers []string,
) ([]string, []string) {
	standard, available := analyzerTermsSignature(
		standardTextAnalyzer,
		queryTermWords(req),
	)
	if !available {
		return analyzers, nil
	}

	branches := make([]string, 0, len(analyzers))
	equivalent := make([]string, 0, len(analyzers))
	for _, analyzer := range analyzers {
		signature, analyzerAvailable := analyzerTermsSignature(
			analyzer,
			queryTermWords(req),
		)
		if analyzerAvailable && signature == standard &&
			!hasAnalyzerDictionaryTerm(analyzer, queryTermWords(req)) {
			equivalent = append(equivalent, analyzer)
			continue
		}
		branches = append(branches, analyzer)
	}

	return branches, equivalent
}

func equivalentAnalyzerScopeClause(analyzers []string) blevequery.Query {
	clauses := make([]blevequery.Query, 0, len(analyzers))
	for _, analyzer := range analyzers {
		clauses = append(clauses, analyzerScopeClause(analyzer))
	}
	if len(clauses) == 1 {
		return clauses[0]
	}

	return bleve.NewDisjunctionQuery(clauses...)
}

func appendEquivalentAnalyzerBranch(
	branches []blevequery.Query,
	branch blevequery.Query,
	equivalentAnalyzers int,
) []blevequery.Query {
	branches = append(branches, branch)
	for range equivalentAnalyzers - 1 {
		branches = append(branches, bleve.NewMatchNoneQuery())
	}

	return branches
}

func analyzerTermsSignature(analyzer string, terms []string) (string, bool) {
	indexMapping := loadStemmingMapping()
	if indexMapping == nil {
		return "", false
	}
	resolved := indexMapping.AnalyzerNamed(analyzer)
	if resolved == nil {
		return "", false
	}

	var signature strings.Builder
	for _, term := range terms {
		tokens := resolved.Analyze([]byte(term))
		signature.WriteString(strconv.Itoa(len(tokens)))
		signature.WriteByte(':')
		for _, token := range tokens {
			signature.WriteString(strconv.Itoa(len(token.Term)))
			signature.WriteByte(':')
			signature.Write(token.Term)
		}
		signature.WriteByte(';')
	}

	return signature.String(), true
}

func hasAnalyzerDictionaryTerm(analyzer string, terms []string) bool {
	for _, term := range terms {
		if cjkDictionaryQueryTerm(analyzer, term) {
			return true
		}
	}

	return false
}
