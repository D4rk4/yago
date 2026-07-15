package searchlocal

import (
	"context"
	"fmt"
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

type localSearcher struct {
	index    searchindex.SearchIndex
	weights  func() searchindex.RankingWeights
	hostRank func() hostrank.AuthorityTable
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
	hostRank func() hostrank.AuthorityTable,
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
	weights := s.currentRankingWeights()
	indexReq := s.indexRequestWithWeights(req, weights)
	resultSet, err := s.searchCandidates(ctx, indexReq)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("search index: %w", err)
	}

	results, err := coreResults(req, resultSet.Results, s.hostRankScorer(req, weights))
	if err != nil {
		return searchcore.Response{}, err
	}

	return searchcore.Response{
		Request:      req,
		TotalResults: resultSet.Total,
		Results:      offsetResults(results, req.Offset, requestLimit(req)),
		Facets:       coreFacets(resultSet.Facets),
	}, nil
}

func (s localSearcher) indexRequest(req searchcore.Request) searchindex.SearchRequest {
	return s.indexRequestWithWeights(req, s.currentRankingWeights())
}

func (s localSearcher) currentRankingWeights() searchindex.RankingWeights {
	if s.weights == nil {
		return searchindex.RankingWeights{}
	}

	return s.weights()
}

func (s localSearcher) indexRequestWithWeights(
	req searchcore.Request,
	weights searchindex.RankingWeights,
) searchindex.SearchRequest {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = strings.Join(req.Terms, " ")
	}

	return searchindex.SearchRequest{
		Query:              query,
		ExcludeTerms:       append([]string(nil), req.ExcludedTerms...),
		Phrases:            append([]string(nil), req.Phrases...),
		MaxResults:         req.Offset + requestLimit(req),
		IncludeDomain:      includeDomains(req),
		SafeSearch:         req.SafeSearch,
		Language:           strings.ToLower(req.Language),
		Weights:            weights,
		Explain:            req.Explain,
		IncludeFieldScores: req.Explain || req.RankingFeatures,
		IncludePositions:   req.Explain || req.RankingFeatures || req.Near,
		Fuzzy:              req.Fuzzy,
		Author:             req.Author,
		Terms:              append([]string(nil), req.Terms...),
		ExpansionTerms:     append([]string(nil), req.ExpansionTerms...),
		Near:               req.Near,
		WithFacets:         req.WithFacets,
		ContentDomain:      string(req.ContentDomain),
		MinDate:            req.MinDate,
		MaxDate:            req.MaxDate,
		FileType:           req.FileType,
		InURL:              req.InURL,
		TLD:                req.TLD,
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
		if filters.match(core) {
			out = append(out, core)
		}
	}
	scorer.rescore(out, req)
	filters.prefer(out)

	return out, nil
}

type hostRankScorer struct {
	hostWeight        float64
	freshWeight       float64
	qualWeight        float64
	urlWeight         float64
	authorityEvidence bool
	freshnessEvidence bool
	table             hostrank.AuthorityTable
	now               time.Time
	freshness         freshnessDecayProfile
}

// URL-length prior constants: weight and saturation length in path runes
// (k≈20 chars per the TREC-13 static feature).
const (
	urlPriorWeight = 0.1
	urlPriorK      = 20.0
)

// hostRankScorer returns a scorer when any prior is enabled by the live
// ranking profile; with every prior weight at zero it returns nil and rescore
// is a no-op.
func (s localSearcher) hostRankScorer(
	req searchcore.Request,
	weights searchindex.RankingWeights,
) *hostRankScorer {
	if s.weights == nil {
		return nil
	}
	evidenceRequested := req.RankingFeatures || req.Explain
	scorer := &hostRankScorer{
		hostWeight:        weights.HostRank,
		freshWeight:       weights.Freshness,
		qualWeight:        weights.Quality,
		urlWeight:         weights.URLPrior,
		authorityEvidence: evidenceRequested,
		freshnessEvidence: evidenceRequested,
		now:               time.Now(),
	}
	// The host table is consulted only when its weight enables it, so a
	// profile with host authority off pays no table snapshot.
	if (scorer.hostWeight > 0 || scorer.authorityEvidence) && s.hostRank != nil {
		scorer.table = s.hostRank()
	}
	if scorer.table == nil && scorer.freshWeight <= 0 && scorer.qualWeight <= 0 &&
		scorer.urlWeight <= 0 && !scorer.freshnessEvidence {
		return nil
	}

	return scorer
}

func (h *hostRankScorer) rescore(results []searchcore.Result, req searchcore.Request) {
	if h == nil {
		return
	}
	if h.freshWeight > 0 || h.freshnessEvidence {
		h.freshness = freshnessProfileFor(req, results, h.now)
	}
	for i := range results {
		results[i].Score *= 1 + h.priors(&results[i])
	}
	if h.hostWeight > 0 || h.freshWeight > 0 || h.qualWeight > 0 || h.urlWeight > 0 {
		sort.SliceStable(results, func(i, j int) bool {
			return results[i].Score > results[j].Score
		})
	}
}

// priors sums the enabled query-independent bonuses for one result.
func (h *hostRankScorer) priors(result *searchcore.Result) float64 {
	urlPrior := urlLengthPrior(result.URL)
	result.Evidence = result.Evidence.With(searchcore.SignalURLPrior, urlPrior)
	total := h.urlWeight * urlPrior
	total += h.authorityPrior(result)
	total += h.freshnessPrior(result)
	if h.qualWeight > 0 && result.QualityKnown {
		total += h.qualWeight * result.Quality
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
		DocumentID:           result.DocumentID,
		Analyzer:             result.Analyzer,
		EvidenceReady:        result.EvidenceReady,
		Title:                result.Title,
		URL:                  result.URL,
		ClusterID:            result.ClusterID,
		RepresentativeURL:    result.RepresentativeURL,
		DisplayURL:           displayURL(host, pathValue),
		Snippet:              result.Snippet,
		Score:                result.Score,
		Evidence:             localRankingEvidence(req, result),
		Source:               req.Source,
		Host:                 host,
		Path:                 pathValue,
		File:                 file,
		ContentType:          result.ContentType,
		URLHash:              hash,
		Size:                 result.Size,
		Date:                 compactPublicationDate(result.PublishedDate),
		DateConfidence:       result.DateConfidence,
		ContentDomain:        req.ContentDomain,
		Language:             result.Language,
		Author:               result.Author,
		Keywords:             result.Keywords,
		Publisher:            result.Publisher,
		Quality:              result.Quality,
		QualityKnown:         result.QualityKnown,
		SpamRisk:             result.SpamRisk,
		FunctionWordFraction: result.FunctionWordFraction,
		SymbolFraction:       result.SymbolFraction,
		AlphabeticFraction:   result.AlphabeticFraction,
		UniqueTokenFraction:  result.UniqueTokenFraction,
		SafetyRating:         searchcore.SafetyRating(result.SafetyRating),
		ExplicitProbability:  result.ExplicitProbability,
		SafetyConfidence:     result.SafetyConfidence,
		Proximity:            result.Proximity,
		FieldScores:          result.FieldScores,
		FieldTermPositions:   result.FieldTermPositions,
		Explanation:          result.Explanation,
		Images:               coreImages(result.Images),
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

func (f resultFilters) match(result searchcore.Result) bool {
	if f.urlMask != nil && !f.urlMask.MatchString(result.URL) {
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
