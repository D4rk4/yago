package documentsearch

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/filetypeclass"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

const searchCoreSnippetRuneCap = 320

type coreLocalSearcher struct {
	searcher  searcher
	documents documentstore.DocumentDirectory
}

type searchCoreResultContext struct {
	ctx       context.Context
	req       searchcore.Request
	documents documentstore.DocumentDirectory
}

func NewLocalSearcher(
	index rwi.PostingIndex,
	urls urlmeta.URLDirectory,
	matchesPerTerm int,
) searchcore.Searcher {
	return NewLocalSearcherWithDocuments(index, urls, nil, matchesPerTerm)
}

func NewLocalSearcherWithDocuments(
	index rwi.PostingIndex,
	urls urlmeta.URLDirectory,
	documents documentstore.DocumentDirectory,
	matchesPerTerm int,
) searchcore.Searcher {
	return coreLocalSearcher{
		searcher: searcher{
			index:          index,
			documents:      urls,
			matchesPerTerm: matchesPerTerm,
		},
		documents: documents,
	}
}

func (s coreLocalSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	criteria, err := searchCoreCriteria(req)
	if err != nil {
		return searchcore.Response{}, err
	}
	result, err := s.searcher.search(ctx, criteria)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("local search: %w", err)
	}
	resp, err := searchCoreResponse(ctx, req, result, s.documents)
	if err != nil {
		return searchcore.Response{}, err
	}

	return resp, nil
}

func searchCoreCriteria(req searchcore.Request) (searchCriteria, error) {
	siteHash, err := siteHashFromCoreRequest(req)
	if err != nil {
		return searchCriteria{}, err
	}

	return searchCriteria{
		terms:             termHashes(req.Terms),
		excludedTerms:     termHashes(req.ExcludedTerms),
		maxResults:        req.Limit + req.Offset,
		reporting:         matchReporting{mode: reportTermWithMostMatches},
		contentKind:       contentKindFromCoreDomain(req.ContentDomain),
		language:          strings.ToLower(req.Language),
		siteHash:          siteHash,
		strictContentKind: req.ContentDomain != searchcore.ContentDomainAll,
		// The searchcore response reports only the join total, never per-term
		// counts, so each scan may stop at the cap.
		allowEarlyTermination: true,
	}, nil
}

func siteHashFromCoreRequest(req searchcore.Request) (string, error) {
	if req.SiteHost == "" {
		return "", nil
	}
	hash, err := yagomodel.HashURLHost(req.SiteHost)
	if err != nil {
		return "", fmt.Errorf("site hash: %w", err)
	}
	hostHash, _ := hash.HostHash()

	return hostHash, nil
}

func termHashes(terms []string) []yagomodel.Hash {
	hashes := make([]yagomodel.Hash, 0, len(terms))
	for _, term := range terms {
		hashes = append(hashes, yagomodel.WordHash(term))
	}

	return hashes
}

func contentKindFromCoreDomain(domain searchcore.ContentDomain) contentKind {
	switch domain {
	case searchcore.ContentDomainImage:
		return imageContent
	case searchcore.ContentDomainAudio:
		return audioContent
	case searchcore.ContentDomainVideo:
		return videoContent
	case searchcore.ContentDomainApp:
		return applicationContent
	default:
		return anyContent
	}
}

func searchCoreResponse(
	ctx context.Context,
	req searchcore.Request,
	result searchResult,
	documents documentstore.DocumentDirectory,
) (searchcore.Response, error) {
	results, err := searchCoreResults(ctx, req, result.resources, documents)
	if err != nil {
		return searchcore.Response{}, err
	}

	resp := searchcore.Response{
		Request:      req,
		TotalResults: result.totalDocumentsMatchingEveryTerm,
		Results:      offsetSearchCoreResults(results, req.Offset, req.Limit),
	}

	return resp, nil
}

func searchCoreResults(
	ctx context.Context,
	req searchcore.Request,
	rows []yagomodel.URIMetadataRow,
	documents documentstore.DocumentDirectory,
) ([]searchcore.Result, error) {
	matchers, err := newCoreResultMatchers(req)
	if err != nil {
		return nil, err
	}
	conversion := searchCoreResultContext{
		ctx:       ctx,
		req:       req,
		documents: documents,
	}
	results := make([]searchcore.Result, 0, len(rows))
	for i, row := range rows {
		result, err := searchCoreResult(conversion, row, i, len(rows))
		if err != nil {
			return nil, err
		}
		if matchers.match(result) {
			results = append(results, result)
		}
	}
	matchers.prefer(results)

	return results, nil
}

func searchCoreResult(
	conversion searchCoreResultContext,
	row yagomodel.URIMetadataRow,
	rank int,
	total int,
) (searchcore.Result, error) {
	rawURL, err := decodedMetadataProperty(conversion.ctx, row, yagomodel.URLMetaURL)
	if err != nil {
		return searchcore.Result{}, err
	}
	title, err := decodedMetadataProperty(conversion.ctx, row, yagomodel.URLMetaColDescription)
	if err != nil {
		return searchcore.Result{}, err
	}
	hash, err := row.URLHash()
	if err != nil {
		return searchcore.Result{}, fmt.Errorf("url metadata hash: %w", err)
	}
	parsed, _ := url.Parse(rawURL)
	host, pathValue, file := parsedURLParts(parsed)
	if title == "" {
		title = rawURL
	}
	snippet := title
	contentType := ""
	safetyRating := searchcore.SafetyUnknown
	explicitProbability := 0.0
	safetyConfidence := 0.0
	clusterID := ""
	representativeURL := ""
	if conversion.documents != nil && rawURL != "" {
		doc, found, err := conversion.documents.Document(conversion.ctx, rawURL)
		if err != nil {
			return searchcore.Result{}, fmt.Errorf("document snippet: %w", err)
		}
		if found {
			clusterID = doc.ClusterID
			representativeURL = doc.RepresentativeURL
			contentType = doc.ContentType
			safetyRating = searchcore.SafetyRating(doc.ContentSafety.Rating)
			explicitProbability = doc.ContentSafety.ExplicitProbability
			safetyConfidence = doc.ContentSafety.Confidence
			if doc.Title != "" {
				title = doc.Title
			}
			if doc.ExtractedText != "" {
				snippet = searchCoreSnippet(doc.ExtractedText)
			} else {
				snippet = title
			}
		}
	}

	return searchcore.Result{
		Title:               title,
		URL:                 rawURL,
		ClusterID:           clusterID,
		RepresentativeURL:   representativeURL,
		DisplayURL:          displayURL(host, pathValue),
		Snippet:             snippet,
		Score:               float64(total - rank),
		Source:              conversion.req.Source,
		Host:                host,
		Path:                pathValue,
		File:                file,
		ContentType:         contentType,
		URLHash:             hash.String(),
		Size:                metadataSize(row),
		Date:                row.Freshness(),
		ContentDomain:       conversion.req.ContentDomain,
		Language:            conversion.req.Language,
		SafetyRating:        safetyRating,
		ExplicitProbability: explicitProbability,
		SafetyConfidence:    safetyConfidence,
	}, nil
}

func searchCoreSnippet(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= searchCoreSnippetRuneCap {
		return text
	}

	return string(runes[:searchCoreSnippetRuneCap])
}

func decodedMetadataProperty(
	ctx context.Context,
	row yagomodel.URIMetadataRow,
	key string,
) (string, error) {
	raw := row.Properties[key]
	if raw == "" {
		return "", nil
	}
	value, err := yagomodel.DecodeWireForm(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("decode url metadata %s: %w", key, err)
	}

	return value, nil
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

func displayURL(host, pathValue string) string {
	if host == "" {
		return pathValue
	}

	return host + pathValue
}

func metadataSize(row yagomodel.URIMetadataRow) int {
	size, _ := strconv.Atoi(row.Properties["size"])

	return size
}

type coreResultMatchers struct {
	urlMask    *regexp.Regexp
	preferMask *regexp.Regexp
	req        searchcore.Request
}

func newCoreResultMatchers(req searchcore.Request) (coreResultMatchers, error) {
	matchers := coreResultMatchers{req: req}
	var err error
	if req.URLMaskFilter != "" {
		matchers.urlMask, err = regexp.Compile(req.URLMaskFilter)
		if err != nil {
			return coreResultMatchers{}, fmt.Errorf("urlmaskfilter: %w", err)
		}
	}
	if req.PreferMaskFilter != "" {
		matchers.preferMask, err = regexp.Compile(req.PreferMaskFilter)
		if err != nil {
			return coreResultMatchers{}, fmt.Errorf("prefermaskfilter: %w", err)
		}
	}

	return matchers, nil
}

func (m coreResultMatchers) match(result searchcore.Result) bool {
	if m.req.SafeSearch && (result.SafetyRating == searchcore.SafetyExplicit ||
		((m.req.ContentDomain == searchcore.ContentDomainImage ||
			result.ContentDomain == searchcore.ContentDomainImage) &&
			result.SafetyRating != searchcore.SafetyGeneral)) {
		return false
	}
	if m.urlMask != nil && !m.urlMask.MatchString(result.URL) {
		return false
	}
	if m.req.InURL != "" &&
		!strings.Contains(strings.ToLower(result.URL), strings.ToLower(m.req.InURL)) {
		return false
	}
	if m.req.TLD != "" && !hostMatchesTLD(result.Host, m.req.TLD) {
		return false
	}
	if m.req.FileType != "" &&
		!filetypeclass.Matches(result.URL, result.ContentType, m.req.FileType) {
		return false
	}

	return true
}

func hostMatchesTLD(host, tld string) bool {
	host = strings.ToLower(host)
	tld = strings.TrimPrefix(strings.ToLower(tld), ".")

	return host == tld || strings.HasSuffix(host, "."+tld)
}

func (m coreResultMatchers) prefer(results []searchcore.Result) {
	if m.preferMask == nil {
		return
	}
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && m.preferMask.MatchString(results[j].URL) &&
			!m.preferMask.MatchString(results[j-1].URL); j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}

func offsetSearchCoreResults(
	results []searchcore.Result,
	offset int,
	limit int,
) []searchcore.Result {
	if offset >= len(results) {
		return nil
	}
	end := offset + limit
	if end > len(results) {
		end = len(results)
	}

	return results[offset:end]
}
