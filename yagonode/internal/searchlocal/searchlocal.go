package searchlocal

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

const defaultMaxResultsPerHost = 5

type localSearcher struct {
	index    searchindex.SearchIndex
	weights  func() searchindex.RankingWeights
	hostRank func() hostrank.Table
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
	return NewSearcherWithRanking(index, weights, nil)
}

// NewSearcherWithRanking extends NewSearcherWithWeights with a live host-authority
// table so the RankingWeights.HostRank coefficient can fold local block-rank into
// result scores. A nil hostRank provider (or a zero HostRank weight) leaves scores
// untouched.
func NewSearcherWithRanking(
	index searchindex.SearchIndex,
	weights func() searchindex.RankingWeights,
	hostRank func() hostrank.Table,
) searchcore.Searcher {
	return localSearcher{index: index, weights: weights, hostRank: hostRank}
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

	results, err := coreResults(req, resultSet.Results, s.hostRankScorer())
	if err != nil {
		return searchcore.Response{}, err
	}

	results = searchcore.DiversifyResults(results, req)
	searchcore.OrderByDateWhenRequested(results, req)

	return searchcore.Response{
		Request:      req,
		TotalResults: resultSet.Total,
		Results:      offsetResults(results, req.Offset, requestLimit(req)),
		Facets:       coreFacets(resultSet.Facets),
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
		Query:            query,
		ExcludeTerms:     append([]string(nil), req.ExcludedTerms...),
		Phrases:          append([]string(nil), req.Phrases...),
		MaxResults:       req.Offset + requestLimit(req),
		IncludeDomain:    includeDomains(req),
		Language:         strings.ToLower(req.Language),
		Weights:          weights,
		IncludePositions: multiTermQuery(query, req.Terms),
		Fuzzy:            req.Fuzzy,
		Author:           req.Author,
		Terms:            append([]string(nil), req.Terms...),
		ExpansionTerms:   append([]string(nil), req.ExpansionTerms...),
		Near:             req.Near,
		WithFacets:       req.WithFacets,
		ContentDomain:    string(req.ContentDomain),
		MinDate:          req.MinDate,
		MaxDate:          req.MaxDate,
	}
}

// coreImages converts the index image refs into the shared search vocabulary.
func coreImages(images []searchindex.ResultImage) []searchcore.ResultImage {
	out := make([]searchcore.ResultImage, 0, len(images))
	for _, image := range images {
		out = append(out, searchcore.ResultImage{URL: image.URL, Alt: image.Alt})
	}

	return out
}

// coreFacets converts the index facet groups into the shared search vocabulary.
func coreFacets(groups []searchindex.FacetGroup) []searchcore.FacetGroup {
	out := make([]searchcore.FacetGroup, 0, len(groups))
	for _, group := range groups {
		terms := make([]searchcore.FacetTerm, 0, len(group.Terms))
		for _, term := range group.Terms {
			terms = append(terms, searchcore.FacetTerm{Term: term.Term, Count: term.Count})
		}
		out = append(out, searchcore.FacetGroup{Name: group.Name, Terms: terms})
	}

	return out
}

// multiTermQuery reports whether the request carries at least two query words,
// the case where matched-term positions add proximity signal; single-term
// queries skip the location cost since proximity needs a pair.
func multiTermQuery(query string, terms []string) bool {
	if len(terms) >= 2 {
		return true
	}

	return len(strings.Fields(query)) >= 2
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
	scorer *hostRankScorer,
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
	scorer.rescore(out)
	filters.prefer(out)

	return diversifyByHost(out, defaultMaxResultsPerHost), nil
}

// hostRankScorer folds the query-independent document priors into result
// scores after retrieval (SEARCH-38): host authority (YBR block rank), a
// freshness decay over the document date, and a saturating URL-length prior
// (Kraaij, Westerveld & Hiemstra, SIGIR 2002: root-like URLs are far likelier
// entry pages; Zaragoza's TREC-13 static feature w·k/(k+len)). Each prior is
// additive inside one multiplier, so relevance stays dominant:
//
//	score ×= 1 + wHost·rank(host) + wFresh·2^(−age/halfLife) + urlPrior(path)
//
// Undated documents skip the freshness term rather than being punished.
type hostRankScorer struct {
	hostWeight  float64
	freshWeight float64
	qualWeight  float64
	proxWeight  float64
	table       hostrank.Table
	now         time.Time
}

// freshnessHalfLife is the age at which the recency prior halves; half a year
// keeps news-ish pages ahead without burying a reference archive.
const freshnessHalfLife = 180 * 24 * time.Hour

// URL-length prior constants: weight and saturation length in path runes
// (k≈20 chars per the TREC-13 static feature).
const (
	urlPriorWeight = 0.1
	urlPriorK      = 20.0
)

// hostRankScorer returns a scorer when any prior is enabled by the live
// ranking profile; with every prior weight at zero it returns nil and rescore
// is a no-op.
func (s localSearcher) hostRankScorer() *hostRankScorer {
	if s.weights == nil {
		return nil
	}
	weights := s.weights()
	scorer := &hostRankScorer{
		hostWeight:  weights.HostRank,
		freshWeight: weights.Freshness,
		qualWeight:  weights.Quality,
		proxWeight:  weights.Proximity,
		now:         time.Now(),
	}
	// The host table is consulted only when its weight enables it, so a
	// profile with host authority off pays no table snapshot.
	if scorer.hostWeight > 0 && s.hostRank != nil {
		scorer.table = s.hostRank()
	}
	if scorer.table == nil && scorer.freshWeight <= 0 &&
		scorer.qualWeight <= 0 && scorer.proxWeight <= 0 {
		return nil
	}

	return scorer
}

func (h *hostRankScorer) rescore(results []searchcore.Result) {
	if h == nil {
		return
	}
	for i := range results {
		results[i].Score *= 1 + h.priors(results[i])
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
}

// priors sums the enabled query-independent bonuses for one result.
func (h *hostRankScorer) priors(result searchcore.Result) float64 {
	total := urlLengthPrior(result.URL)
	if h.hostWeight > 0 && h.table != nil {
		total += h.hostWeight * h.table.Rank(hostHashOf(result.URL))
	}
	if h.freshWeight > 0 {
		if published, err := time.Parse("20060102", result.Date); err == nil {
			age := h.now.Sub(published)
			if age < 0 {
				age = 0
			}
			total += h.freshWeight * math.Exp2(-age.Hours()/freshnessHalfLife.Hours())
		}
	}
	if h.qualWeight > 0 {
		total += h.qualWeight * result.Quality
	}
	if h.proxWeight > 0 {
		total += h.proxWeight * result.Proximity
	}

	return total
}

// urlLengthPrior is the saturating root-URL bonus: an empty or short path
// earns up to urlPriorWeight, a deep path almost nothing.
func urlLengthPrior(rawURL string) float64 {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	pathLen := float64(len(strings.Trim(parsed.Path, "/")))

	return urlPriorWeight * urlPriorK / (urlPriorK + pathLen)
}

// hostHashOf derives the YaCy host hash of rawURL the same way the host-link graph
// does (HashURL then HostHash), yielding "" for a URL that cannot be hashed so the
// lookup falls back to a neutral rank.
func hostHashOf(rawURL string) string {
	urlHash, _ := yagomodel.HashURL(rawURL)
	hostHash, _ := urlHash.HostHash()

	return hostHash
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
		Title:              result.Title,
		URL:                result.URL,
		DisplayURL:         displayURL(host, pathValue),
		Snippet:            result.Snippet,
		Score:              result.Score,
		Source:             req.Source,
		Host:               host,
		Path:               pathValue,
		File:               file,
		URLHash:            hash,
		Date:               result.PublishedDate.Format("20060102"),
		ContentDomain:      req.ContentDomain,
		Language:           req.Language,
		Author:             result.Author,
		Keywords:           result.Keywords,
		Publisher:          result.Publisher,
		Quality:            result.Quality,
		Proximity:          result.Proximity,
		FieldScores:        result.FieldScores,
		FieldTermPositions: result.FieldTermPositions,
		Images:             coreImages(result.Images),
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
