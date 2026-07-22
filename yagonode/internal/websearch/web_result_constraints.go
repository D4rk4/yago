package websearch

import (
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/filetypeclass"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/sitehost"
)

func resultsMatchingConstraints(req searchcore.Request, results []Result) []Result {
	kept := make([]Result, 0, len(results))
	for _, result := range results {
		if resultMatchesConstraints(req, result) {
			kept = append(kept, result)
		}
	}

	return kept
}

func resultMatchesConstraints(req searchcore.Request, result Result) bool {
	if !searchcore.ResultSatisfiesDomainConstraints(req, searchcore.Result{URL: result.URL}) {
		return false
	}
	if len(req.ExcludedTerms) > 0 && searchcore.ResultMentionsTerms(searchcore.Result{
		Title: result.Title, URL: result.URL, Snippet: result.Snippet,
	}, req.ExcludedTerms) {
		return false
	}
	if req.InURL != "" && !strings.Contains(
		strings.ToLower(result.URL),
		strings.ToLower(req.InURL),
	) {
		return false
	}
	if req.FileType != "" && !filetypeclass.Matches(result.URL, "", req.FileType) {
		return false
	}
	if req.SiteHost == "" && req.TLD == "" {
		return true
	}
	parsed, err := url.Parse(result.URL)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := parsed.Hostname()
	if req.SiteHost != "" && !sitehost.Matches(host, req.SiteHost) {
		return false
	}

	return req.TLD == "" || hostMatchesDomain(host, req.TLD)
}

func hostMatchesDomain(host, domain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	domain = strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))

	return host != "" && domain != "" &&
		(host == domain || strings.HasSuffix(host, "."+domain))
}
