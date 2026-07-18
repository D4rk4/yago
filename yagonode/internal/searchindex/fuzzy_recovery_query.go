package searchindex

import (
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
)

const maximumFuzzyTermRunes = 64

func fuzzyRecoveryQuery(
	req SearchRequest,
	analyzers []string,
	weights RankingWeights,
	analyzerScope bool,
) blevequery.Query {
	if !analyzerScope {
		return strictFuzzyRecoveryQuery(req, analyzers, weights)
	}
	standard := strictFuzzyRecoveryQuery(
		req,
		[]string{standardTextAnalyzer},
		weights,
	)
	branches := []blevequery.Query{standard}
	branchAnalyzers, equivalentAnalyzers := requirementAnalyzerBranches(req, analyzers)
	if len(equivalentAnalyzers) > 0 {
		branch, found := fuzzyAnalyzerBranch(
			req,
			standardTextAnalyzer,
			equivalentAnalyzerScopeClause(equivalentAnalyzers),
			weights,
		)
		if found {
			branches = appendEquivalentAnalyzerBranch(
				branches,
				branch,
				len(equivalentAnalyzers),
			)
		}
	}
	for _, analyzer := range branchAnalyzers {
		branch, found := fuzzyAnalyzerBranch(
			req,
			analyzer,
			analyzerScopeClause(analyzer),
			weights,
		)
		if found {
			branches = append(branches, branch)
		}
	}
	if len(branches) == 1 {
		return branches[0]
	}

	return bleve.NewDisjunctionQuery(branches...)
}

func fuzzyAnalyzerBranch(
	req SearchRequest,
	analyzer string,
	scope blevequery.Query,
	weights RankingWeights,
) (blevequery.Query, bool) {
	terms := requirableTermsForAnalyzer(queryTermWords(req), analyzer)
	if len(terms) == 0 {
		return nil, false
	}
	required := make([]blevequery.Query, 0, len(terms)+1)
	required = append(required, scope)
	for _, term := range terms {
		required = append(
			required,
			fuzzyCrossFieldTermClauseForAnalyzer(term, analyzer, weights),
		)
	}

	return bleve.NewConjunctionQuery(required...), true
}

func strictFuzzyRecoveryQuery(
	req SearchRequest,
	analyzers []string,
	weights RankingWeights,
) blevequery.Query {
	terms := requirableTerms(queryTermWords(req), analyzers)
	if len(terms) == 0 {
		terms = []string{req.Query}
	}
	required := make([]blevequery.Query, 0, len(terms))
	for _, term := range terms {
		required = append(required, fuzzyCrossFieldTermClause(term, analyzers, weights))
	}
	if len(required) == 1 {
		return required[0]
	}

	return bleve.NewConjunctionQuery(required...)
}

func fuzzyCrossFieldTermClauseForAnalyzer(
	term string,
	analyzer string,
	weights RankingWeights,
) blevequery.Query {
	clause := bleve.NewDisjunctionQuery()
	prefix := fuzzyPrefixLength(term)
	distance := fuzzyEditDistance(term)
	for _, field := range textSearchFields() {
		match := fieldMatchWithAnalyzer(
			field,
			term,
			textFieldWeight(field, weights),
			analyzer,
		)
		match.SetFuzziness(distance)
		match.SetPrefix(prefix)
		clause.AddQuery(match)
	}
	urlMatch := fieldMatch("url", term, weights.URL)
	urlMatch.SetFuzziness(distance)
	urlMatch.SetPrefix(prefix)
	clause.AddQuery(urlMatch)

	return clause
}

func fuzzyCrossFieldTermClause(
	term string,
	analyzers []string,
	weights RankingWeights,
) blevequery.Query {
	clause := bleve.NewDisjunctionQuery()
	prefix := fuzzyPrefixLength(term)
	distance := fuzzyEditDistance(term)
	for _, analyzer := range analyzers {
		for _, field := range textSearchFields() {
			match := fieldMatchWithAnalyzer(
				field,
				term,
				textFieldWeight(field, weights),
				analyzer,
			)
			match.SetFuzziness(distance)
			match.SetPrefix(prefix)
			clause.AddQuery(match)
		}
	}
	urlMatch := fieldMatch("url", term, weights.URL)
	urlMatch.SetFuzziness(distance)
	urlMatch.SetPrefix(prefix)
	clause.AddQuery(urlMatch)

	return clause
}

func fuzzyEditDistance(term string) int {
	switch runes := utf8.RuneCountInString(term); {
	case runes <= 2 || runes > maximumFuzzyTermRunes:
		return 0
	case runes >= 8:
		return 2
	}

	return 1
}

func fuzzyPrefixLength(term string) int {
	desiredRunes := 0
	switch runes := utf8.RuneCountInString(term); {
	case fuzzyEditDistance(term) == 2:
		desiredRunes = 4
	case runes >= 6:
		desiredRunes = 2
	case runes >= 4:
		desiredRunes = 1
	}
	bytes := 0
	for _, character := range term {
		if desiredRunes == 0 {
			break
		}
		bytes += utf8.RuneLen(character)
		desiredRunes--
	}

	return bytes
}
