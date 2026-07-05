package searchlocal

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

const defaultMaxResultsPerHost = 5

type localSearcher struct {
	index   searchindex.SearchIndex
	weights func() searchindex.RankingWeights
}

func NewSearcher(index searchindex.SearchIndex) searchcore.Searcher {
	return NewSearcherWithWeights(index, nil)
}

// NewSearcherWithWeights builds a local searcher that reads its ranking weights
// from weights on every request, so an operator's persisted ranking profile
// applies live. A nil provider keeps the built-in default weights.
func NewSearcherWithWeights(
	index searchindex.SearchIndex,
	weights func() searchindex.RankingWeights,
) searchcore.Searcher {
	return localSearcher{index: index, weights: weights}
}

func (s localSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if s.index == nil {
		return searchcore.Response{}, fmt.Errorf("search index unavailable")
	}
	indexReq := s.indexRequest(req)
	resultSet, err := s.index.Search(ctx, indexReq)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("search index: %w", err)
	}

	results, err := coreResults(req, resultSet.Results)
	if err != nil {
		return searchcore.Response{}, err
	}

	searchcore.OrderByDateWhenRequested(results, req)

	return searchcore.Response{
		Request:      req,
		TotalResults: resultSet.Total,
		Results:      offsetResults(results, req.Offset, requestLimit(req)),
	}, nil
}

func (s localSearcher) indexRequest(req searchcore.Request) searchindex.SearchRequest {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = strings.Join(req.Terms, " ")
	}

	var weights searchindex.RankingWeights
	if s.weights != nil {
		weights = s.weights()
	}

	return searchindex.SearchRequest{
		Query:         query,
		ExcludeTerms:  append([]string(nil), req.ExcludedTerms...),
		Phrases:       append([]string(nil), req.Phrases...),
		MaxResults:    req.Offset + requestLimit(req),
		IncludeDomain: includeDomains(req),
		Language:      strings.ToLower(req.Language),
		Weights:       weights,
	}
}

func requestLimit(req searchcore.Request) int {
	if req.Limit <= 0 {
		return searchcore.DefaultPublicLimit
	}

	return req.Limit
}

func includeDomains(req searchcore.Request) []string {
	if req.SiteHost == "" {
		return nil
	}

	return []string{req.SiteHost}
}

func coreResults(
	req searchcore.Request,
	results []searchindex.SearchResult,
) ([]searchcore.Result, error) {
	filters, err := requestFilters(req)
	if err != nil {
		return nil, err
	}
	out := make([]searchcore.Result, 0, len(results))
	for _, result := range results {
		core := coreResult(req, result)
		if filters.match(req, core) {
			out = append(out, core)
		}
	}
	filters.prefer(out)

	return diversifyByHost(out, defaultMaxResultsPerHost), nil
}

func diversifyByHost(results []searchcore.Result, maxPerHost int) []searchcore.Result {
	counts := make(map[string]int, len(results))
	kept := make([]searchcore.Result, 0, len(results))
	var overflow []searchcore.Result
	for _, result := range results {
		host := strings.ToLower(result.Host)
		if host == "" || counts[host] < maxPerHost {
			counts[host]++
			kept = append(kept, result)

			continue
		}
		overflow = append(overflow, result)
	}

	return append(kept, overflow...)
}

func coreResult(
	req searchcore.Request,
	result searchindex.SearchResult,
) searchcore.Result {
	parsed, _ := url.Parse(result.URL)
	host, pathValue, file := parsedURLParts(parsed)
	hash := ""
	if urlHash, err := yagomodel.HashURL(result.URL); err == nil {
		hash = urlHash.String()
	}

	return searchcore.Result{
		Title:         result.Title,
		URL:           result.URL,
		DisplayURL:    displayURL(host, pathValue),
		Snippet:       result.Snippet,
		Score:         result.Score,
		Source:        req.Source,
		Host:          host,
		Path:          pathValue,
		File:          file,
		URLHash:       hash,
		Date:          result.PublishedDate.Format("20060102"),
		ContentDomain: req.ContentDomain,
		Language:      req.Language,
	}
}

type resultFilters struct {
	urlMask       *regexp.Regexp
	preferPattern *regexp.Regexp
}

func requestFilters(req searchcore.Request) (resultFilters, error) {
	var filters resultFilters
	if req.URLMaskFilter != "" {
		pattern, err := regexp.Compile(req.URLMaskFilter)
		if err != nil {
			return resultFilters{}, fmt.Errorf("urlmaskfilter: %w", err)
		}
		filters.urlMask = pattern
	}
	if req.PreferMaskFilter != "" {
		pattern, err := regexp.Compile(req.PreferMaskFilter)
		if err != nil {
			return resultFilters{}, fmt.Errorf("prefermaskfilter: %w", err)
		}
		filters.preferPattern = pattern
	}

	return filters, nil
}

func (f resultFilters) match(req searchcore.Request, result searchcore.Result) bool {
	if f.urlMask != nil && !f.urlMask.MatchString(result.URL) {
		return false
	}
	if req.InURL != "" &&
		!strings.Contains(strings.ToLower(result.URL), strings.ToLower(req.InURL)) {
		return false
	}
	if req.TLD != "" && !hostMatchesTLD(result.Host, req.TLD) {
		return false
	}
	if req.FileType != "" && !fileMatchesType(result.File, req.FileType) {
		return false
	}

	return true
}

func (f resultFilters) prefer(results []searchcore.Result) {
	if f.preferPattern == nil {
		return
	}
	sort.SliceStable(results, func(i, j int) bool {
		return f.preferPattern.MatchString(results[i].URL) &&
			!f.preferPattern.MatchString(results[j].URL)
	})
}

func hostMatchesTLD(host string, tld string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	tld = strings.TrimPrefix(strings.ToLower(tld), ".")

	return host == tld || strings.HasSuffix(host, "."+tld)
}

func fileMatchesType(file string, fileType string) bool {
	return strings.TrimPrefix(strings.ToLower(path.Ext(file)), ".") ==
		strings.TrimPrefix(strings.ToLower(fileType), ".")
}

func parsedURLParts(parsed *url.URL) (string, string, string) {
	if parsed == nil {
		return "", "", ""
	}
	pathValue := parsed.EscapedPath()
	file := path.Base(parsed.Path)
	if file == "." || file == "/" {
		file = ""
	}

	return parsed.Hostname(), pathValue, file
}

func displayURL(host string, pathValue string) string {
	if host == "" {
		return pathValue
	}

	return host + pathValue
}

func offsetResults(results []searchcore.Result, offset int, limit int) []searchcore.Result {
	if offset >= len(results) {
		return nil
	}
	end := offset + limit
	if end > len(results) {
		end = len(results)
	}

	return results[offset:end]
}
