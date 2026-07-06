package searchindex

import (
	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
)

// fuzzyRecoveryQuery is the recall-first query the zero-result recovery retry
// runs: the whole query text ORed across every candidate analyzer's text fields
// and the url field, tolerating one edit per term so slightly misspelled
// queries still reach close matches. It runs only when the precise query found
// nothing, so its looseness cannot flood an ordinary answer.
func fuzzyRecoveryQuery(
	req SearchRequest,
	gram bool,
	analyzers []string,
	weights RankingWeights,
) blevequery.Query {
	main := bleve.NewDisjunctionQuery()
	for _, analyzer := range analyzers {
		for _, field := range textSearchFields() {
			match := fieldMatchWithAnalyzer(
				field,
				req.Query,
				textFieldWeight(field, weights),
				analyzer,
			)
			match.SetFuzziness(1)
			main.AddQuery(match)
		}
	}
	urlMatch := fieldMatch("url", req.Query, weights.URL)
	urlMatch.SetFuzziness(1)
	main.AddQuery(urlMatch)
	if gram {
		// Language-agnostic trigram recall, restricted to the zero-result
		// recovery path. Matching a word's character trigrams with AND semantics
		// does NOT require them to be contiguous or in one word, so over a long
		// body every common trigram of a query word (e.g. Russian "черногория" ->
		// чер, ерн, рно, ...) occurs scattered in nearly every same-script
		// document, flooding ordinary queries with unrelated content. Restoring
		// contiguity needs positional term vectors (a reindex), and morphology is
		// better served by per-language stemming, so grams now only widen recall
		// when the precise query already found nothing rather than on every search.
		// Skipped when the index mapping predates the trigram analyzer: such an
		// index can neither resolve the analyzer nor hold gram fields, and a
		// query referencing it fails the whole search.
		main.AddQuery(
			gramMatch("title"+gramFieldSuffix, req.Query, weights.Title*gramWeightFactor),
			gramMatch("headings"+gramFieldSuffix, req.Query, weights.Headings*gramWeightFactor),
			gramMatch("anchors"+gramFieldSuffix, req.Query, weights.Anchors*gramWeightFactor),
			gramMatch("body"+gramFieldSuffix, req.Query, weights.Body*gramWeightFactor),
		)
	}

	return main
}
