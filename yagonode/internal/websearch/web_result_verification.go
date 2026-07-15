package websearch

import (
	"math"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func verifiedWebResults(req searchcore.Request, results []Result) []Result {
	if req.Verify != searchcore.VerifyFalse {
		terms := req.Terms
		if len(terms) == 0 {
			terms = searchcore.ParseTextQuery(req.Query).Terms
		}
		results = resultsMentioningTerms(terms, results)
	}

	return resultsMatchingConstraints(req, results)
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
