package websearch

import (
	"math"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func verifiedWebResults(req searchcore.Request, results []Result) []Result {
	return resultsMatchingConstraints(req, relevantWebResults(req, results))
}

// relevantWebResults keeps the provider rows that answer the query, and stops
// there. It is the half of verification that crawl seeding must respect: a row
// the engines returned for an unrelated page is not a discovery, it is noise,
// and seeding it would crawl whatever a provider happened to pad its answer
// with.
//
// The other half, resultsMatchingConstraints, applies what this caller asked to
// be served -- an include or exclude domain, an excluded term, an inurl. Those
// say nothing about whether a page is worth having. Gating seeding on them made
// a constrained request discover nothing at all, silently, because the seeder
// cannot tell an empty list from a provider that returned none.
func relevantWebResults(req searchcore.Request, results []Result) []Result {
	if req.Verify == searchcore.VerifyFalse {
		return results
	}
	terms := req.Terms
	if len(terms) == 0 {
		terms = searchcore.ParseTextQuery(req.Query).Terms
	}

	return resultsMentioningTerms(terms, results)
}

func VerifiedForQuery(query string, results []Result) []Result {
	req := searchcore.RequestWithParsedQuery(searchcore.Request{Query: query})

	return verifiedWebResults(req, results)
}

func resultsMentioningTerms(terms []string, results []Result) []Result {
	minimumCoverage := minimumWebTermCoverage(terms)
	kept := make([]Result, 0, len(results))
	for _, result := range results {
		mention := searchcore.Result{
			Title:   result.Title,
			Snippet: result.Snippet,
			URL:     result.URL,
		}
		if resultCoversTerms(mention, terms, minimumCoverage) {
			kept = append(kept, result)
		}
	}

	return kept
}

func minimumWebTermCoverage(terms []string) int {
	distinct := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term != "" {
			distinct[term] = struct{}{}
		}
	}
	total := len(distinct)
	if total < 3 {
		return total
	}
	minimum := int(math.Ceil(float64(total) * 0.6))

	return max(1, min(total-1, minimum))
}

func resultCoversTerms(result searchcore.Result, terms []string, minimum int) bool {
	return resultHasExactIdentifiers(result, terms) &&
		(minimum <= 0 || coveredDistinctTerms(result, terms) >= minimum)
}
