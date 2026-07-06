package websearch

import (
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

// verifiedWebResults keeps the provider hits whose own title, snippet, or URL
// mentions a query term, dropping the rest before they are displayed or seeded
// to the crawler — the same containment gate peer results pass, because an
// engine consent page or anti-bot filler otherwise flows straight into the
// SERP and, via greedy-learning, into the local index. Verify=false trusts the
// provider verbatim.
func verifiedWebResults(req searchcore.Request, results []Result) []Result {
	if req.Verify == searchcore.VerifyFalse {
		return results
	}
	terms := req.Terms
	if len(terms) == 0 {
		terms = strings.Fields(req.Query)
	}
	kept := make([]Result, 0, len(results))
	for _, result := range results {
		mention := searchcore.Result{
			Title:   result.Title,
			Snippet: result.Snippet,
			URL:     result.URL,
		}
		if searchcore.ResultMentionsTerms(mention, terms) {
			kept = append(kept, result)
		}
	}

	return kept
}
