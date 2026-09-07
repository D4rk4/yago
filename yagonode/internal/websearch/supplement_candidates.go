package websearch

import (
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const SupplementalCandidateTarget = 100

func supplementalWindow(request searchcore.Request) searchcore.Request {
	request.Limit = min(searchcore.MaximumPublicResultHorizon,
		max(SupplementalCandidateTarget, request.Offset+request.Limit))
	request.Offset = 0
	return request
}

func candidateURL(raw string) string {
	canonical, valid := yagocrawlcontract.CanonicalURL(raw)
	if valid {
		return canonical
	}
	return raw
}

func primaryCandidates(
	request searchcore.Request,
	response searchcore.Response,
) (searchcore.Response, map[string]struct{}) {
	original := len(response.Results)
	seen := make(map[string]struct{}, len(response.Results))
	kept := make([]searchcore.Result, 0, len(response.Results))
	for _, result := range response.Results {
		identity := candidateURL(result.URL)
		if identity == "" || !resultMatchesConstraints(request, Result{
			URL: result.URL, Title: result.Title, Snippet: result.Snippet,
		}) || !searchcore.ResultSatisfiesSafeSearch(request, result) {
			continue
		}
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		kept = append(kept, result)
	}
	response.Results = searchcore.ConsolidateClusters(kept)
	response.TotalResults = max(
		len(response.Results),
		response.TotalResults-original+len(response.Results),
	)
	return response, seen
}

func supplementalPage(
	response searchcore.Response,
	request searchcore.Request,
) searchcore.Response {
	response.Request = request
	limit := request.Limit
	if limit <= 0 {
		limit = searchcore.DefaultPublicLimit
	}
	start := min(request.Offset, len(response.Results))
	response.Results = response.Results[start:min(start+limit, len(response.Results))]
	return response
}

func withoutKnownResults(results []Result, known map[string]struct{}) []Result {
	kept := make([]Result, 0, len(results))
	seen := make(map[string]struct{}, len(known)+len(results))
	for key := range known {
		seen[key] = struct{}{}
	}
	for _, result := range results {
		identity := candidateURL(result.URL)
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		kept = append(kept, result)
	}
	return kept
}
