package searchindex

import (
	"strings"

	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
)

// expansionBoostFactor scales the optional expansion clauses relative to the
// required term clauses, mirroring the RM3 interpolation weight Anserini
// defaults to (originalQueryWeight 0.5), so mined terms break ties without
// overpowering the query the user typed.
const expansionBoostFactor = 0.5

// requiredTermsQuery builds the precise (non-fuzzy) retrieval query: every
// query word must appear somewhere in the document — each term is a
// cross-field disjunction and the terms are joined conjunctively — matching
// the all-words guarantee YaCy's RWI join (TermSearch.joined) gives, where a
// URL survives only if every include word's posting list holds it. Words a
// candidate analyzer folds away entirely (stopwords of the query's script
// languages) are exempt from the conjunction: their tokens were never indexed,
// so requiring them would veto every document. Expansion terms attach as
// optional weighted clauses that reorder but never admit.
func requiredTermsQuery(
	req SearchRequest,
	analyzers []string,
	weights RankingWeights,
) blevequery.Query {
	terms := requirableTerms(queryTermWords(req), analyzers)
	if len(terms) == 0 {
		return crossFieldTermClause(req.Query, analyzers, weights, 1)
	}
	required := make([]blevequery.Query, 0, len(terms))
	for _, term := range terms {
		required = append(required, crossFieldTermClause(term, analyzers, weights, 1))
	}
	main := required[0]
	if len(required) > 1 {
		main = blevequery.Query(bleve.NewConjunctionQuery(required...))
	}
	if len(req.ExpansionTerms) == 0 {
		return main
	}

	query := bleve.NewBooleanQuery()
	query.AddMust(main)
	for _, term := range req.ExpansionTerms {
		query.AddShould(crossFieldTermClause(term, analyzers, weights, expansionBoostFactor))
	}

	return query
}

// crossFieldTermClause matches one term (or the raw query text, for the
// all-stopwords fallback) anywhere in a document: every candidate analyzer's
// stemmed text fields plus the url field, as one disjunction.
func crossFieldTermClause(
	text string,
	analyzers []string,
	weights RankingWeights,
	factor float64,
) blevequery.Query {
	clause := bleve.NewDisjunctionQuery()
	for _, analyzer := range analyzers {
		for _, field := range textSearchFields() {
			clause.AddQuery(fieldMatchWithAnalyzer(
				field,
				text,
				textFieldWeight(field, weights)*factor,
				analyzer,
			))
		}
	}
	clause.AddQuery(fieldMatch("url", text, weights.URL*factor))

	return clause
}

// queryTermWords is the parsed query words, falling back to whitespace
// splitting when the caller sent no parsed terms.
func queryTermWords(req SearchRequest) []string {
	if len(req.Terms) > 0 {
		return req.Terms
	}

	return strings.Fields(req.Query)
}

// requirableTerms keeps the words that may be demanded of every document,
// dropping those some candidate analyzer folds away entirely.
func requirableTerms(terms []string, analyzers []string) []string {
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" || analyzedAway(term, analyzers) {
			continue
		}
		out = append(out, term)
	}

	return out
}

// analyzedAway reports whether any candidate stemming analyzer analyzes the
// term to nothing — a stopword of a plausible query language. Such a word was
// stripped from same-language documents at index time, so a mandatory clause
// for it would exclude exactly the documents it should find. The standard
// analyzer keeps every word and carries no stopword signal, so it is skipped;
// with no resolvable mapping every term stays requirable.
func analyzedAway(term string, analyzers []string) bool {
	indexMapping := loadStemmingMapping()
	if indexMapping == nil {
		return false
	}
	for _, name := range analyzers {
		if name == "" || name == standardTextAnalyzer {
			continue
		}
		if len(indexMapping.AnalyzerNamed(name).Analyze([]byte(term))) == 0 {
			return true
		}
	}

	return false
}
