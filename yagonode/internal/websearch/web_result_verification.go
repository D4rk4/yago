package websearch

import (
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/stopwords"
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
	if content := stopwords.ContentTerms(terms); len(content) > 0 {
		terms = content
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
