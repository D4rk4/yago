package tavilyapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/documentstore"
	"github.com/D4rk4/yago/yacynode/internal/searchcore"
)

const (
	PathSearch        = "/search"
	defaultMaxResults = 5
	maxResultsCap     = 20
	snippetRuneCap    = 320
)

type searchEndpoint struct {
	search    searchcore.Searcher
	documents documentstore.DocumentDirectory
	now       func() time.Time
}

type SearchRequest struct {
	Query             string        `json:"query"`
	SearchDepth       string        `json:"search_depth,omitempty"`
	MaxResults        *int          `json:"max_results,omitempty"`
	IncludeAnswer     inclusionMode `json:"include_answer,omitempty"`
	IncludeRawContent bool          `json:"include_raw_content,omitempty"`
	IncludeDomains    []string      `json:"include_domains,omitempty"`
	ExcludeDomains    []string      `json:"exclude_domains,omitempty"`
	Topic             string        `json:"topic,omitempty"`
	TimeRange         string        `json:"time_range,omitempty"`
	SafeSearch        bool          `json:"safe_search,omitempty"`
}

type inclusionMode string

type SearchResponse struct {
	Query        string         `json:"query"`
	Answer       string         `json:"answer,omitempty"`
	Results      []SearchResult `json:"results"`
	ResponseTime float64        `json:"response_time"`
}

type SearchResult struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	Content       string   `json:"content"`
	RawContent    *string  `json:"raw_content,omitempty"`
	Score         float64  `json:"score"`
	PublishedDate string   `json:"published_date,omitempty"`
	Source        string   `json:"source,omitempty"`
	Images        []string `json:"images,omitempty"`
}

func Mount(
	mux *http.ServeMux,
	search searchcore.Searcher,
	documents documentstore.DocumentDirectory,
) {
	mux.Handle(PathSearch, NewSearchEndpoint(search, documents))
}

func NewSearchEndpoint(
	search searchcore.Searcher,
	documents documentstore.DocumentDirectory,
) http.Handler {
	return searchEndpoint{
		search:    search,
		documents: documents,
		now:       time.Now,
	}
}

func (e searchEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SearchRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid search request", http.StatusBadRequest)
		return
	}

	start := e.now()
	resp, err := e.searchResponse(r.Context(), req, start)
	if err != nil {
		status := http.StatusInternalServerError
		if isBadRequest(err) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (e searchEndpoint) searchResponse(
	ctx context.Context,
	req SearchRequest,
	start time.Time,
) (SearchResponse, error) {
	coreReq, err := coreRequest(req)
	if err != nil {
		return SearchResponse{}, err
	}
	if coreReq.Limit == 0 {
		return SearchResponse{
			Query:        strings.TrimSpace(req.Query),
			Results:      []SearchResult{},
			ResponseTime: e.now().Sub(start).Seconds(),
		}, nil
	}

	resp, err := e.search.Search(ctx, coreReq)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("search failed: %w", err)
	}

	results, err := e.responseResults(ctx, req, coreReq, resp.Results)
	if err != nil {
		return SearchResponse{}, err
	}

	return SearchResponse{
		Query:        coreReq.Query,
		Results:      results,
		ResponseTime: e.now().Sub(start).Seconds(),
	}, nil
}

func coreRequest(req SearchRequest) (searchcore.Request, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return searchcore.Request{}, badRequest("query is required")
	}
	if err := validateRequestOptions(req); err != nil {
		return searchcore.Request{}, err
	}

	limit, err := requestLimit(req.MaxResults)
	if err != nil {
		return searchcore.Request{}, err
	}
	source, err := sourceForDepth(req.SearchDepth)
	if err != nil {
		return searchcore.Request{}, err
	}
	parsed := searchcore.ParseTextQuery(query)

	coreReq, _ := searchcore.NormalizePublicRequest(searchcore.Request{
		Query:         query,
		Terms:         parsed.Terms,
		ExcludedTerms: parsed.ExcludedTerms,
		Source:        source,
		Limit:         limit,
		ContentDomain: searchcore.ContentDomainText,
		Language:      parsed.Language,
		SiteHost:      firstDomain(req.IncludeDomains),
		InURL:         parsed.InURL,
		TLD:           parsed.TLD,
		FileType:      parsed.FileType,
		Verify:        searchcore.VerifyFalse,
		SortByDate:    parsed.SortByDate,
		Near:          parsed.Near,
	}, maxResultsCap)
	if limit == 0 {
		coreReq.Limit = 0
	}

	return coreReq, nil
}

func requestLimit(maxResults *int) (int, error) {
	if maxResults == nil {
		return defaultMaxResults, nil
	}
	if *maxResults < 0 || *maxResults > maxResultsCap {
		return 0, badRequest("max_results must be between 0 and 20")
	}

	return *maxResults, nil
}

func sourceForDepth(depth string) (searchcore.Source, error) {
	switch strings.ToLower(strings.TrimSpace(depth)) {
	case "", "basic", "fast", "ultra-fast":
		return searchcore.SourceLocal, nil
	case "advanced":
		return searchcore.SourceGlobal, nil
	default:
		return "", badRequest("unsupported search_depth")
	}
}

func validateRequestOptions(req SearchRequest) error {
	if err := validateTopic(req.Topic); err != nil {
		return err
	}
	if err := validateTimeRange(req.TimeRange); err != nil {
		return err
	}
	for _, domain := range append(req.IncludeDomains, req.ExcludeDomains...) {
		if normalizeDomain(domain) == "" {
			return badRequest("domain filters must be host names")
		}
	}

	return nil
}

func validateTopic(topic string) error {
	switch strings.ToLower(strings.TrimSpace(topic)) {
	case "", "general", "news", "finance":
		return nil
	default:
		return badRequest("unsupported topic")
	}
}

func validateTimeRange(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "day", "d", "week", "w", "month", "m", "year", "y":
		return nil
	default:
		return badRequest("unsupported time_range")
	}
}

func (e searchEndpoint) responseResults(
	ctx context.Context,
	req SearchRequest,
	coreReq searchcore.Request,
	results []searchcore.Result,
) ([]SearchResult, error) {
	out := make([]SearchResult, 0, len(results))
	for _, result := range results {
		if !allowsDomain(result, req.IncludeDomains, req.ExcludeDomains) {
			continue
		}

		item, err := e.responseResult(ctx, req, coreReq, result)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}

	return out, nil
}

func (e searchEndpoint) responseResult(
	ctx context.Context,
	req SearchRequest,
	coreReq searchcore.Request,
	result searchcore.Result,
) (SearchResult, error) {
	content := result.Snippet
	var raw *string
	doc, found, err := e.document(ctx, result.URL)
	if err != nil {
		return SearchResult{}, err
	}
	if found {
		if doc.Title != "" {
			result.Title = doc.Title
		}
		if doc.ExtractedText != "" {
			content = snippet(doc.ExtractedText)
			if req.IncludeRawContent {
				raw = &doc.ExtractedText
			}
		}
	}
	if content == "" {
		content = result.Title
	}

	return SearchResult{
		Title:         result.Title,
		URL:           result.URL,
		Content:       content,
		RawContent:    raw,
		Score:         result.Score,
		PublishedDate: result.Date,
		Source:        string(coreReq.Source),
	}, nil
}

func (e searchEndpoint) document(
	ctx context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	if e.documents == nil || normalizedURL == "" {
		return documentstore.Document{}, false, nil
	}
	doc, found, err := e.documents.Document(ctx, normalizedURL)
	if err != nil {
		return documentstore.Document{}, false, fmt.Errorf("document lookup failed: %w", err)
	}

	return doc, found, nil
}

func allowsDomain(result searchcore.Result, includeDomains, excludeDomains []string) bool {
	host := resultHost(result)
	for _, domain := range excludeDomains {
		if domainMatches(host, domain) {
			return false
		}
	}
	if len(includeDomains) == 0 {
		return true
	}
	for _, domain := range includeDomains {
		if domainMatches(host, domain) {
			return true
		}
	}

	return false
}

func resultHost(result searchcore.Result) string {
	if result.Host != "" {
		return strings.ToLower(result.Host)
	}
	parsed, _ := url.Parse(result.URL)

	return strings.ToLower(parsed.Hostname())
}

func domainMatches(host, domain string) bool {
	domain = normalizeDomain(domain)
	if host == "" || domain == "" {
		return false
	}

	return host == domain || strings.HasSuffix(host, "."+domain)
}

func firstDomain(domains []string) string {
	for _, domain := range domains {
		if normalized := normalizeDomain(domain); normalized != "" {
			return normalized
		}
	}

	return ""
}

func normalizeDomain(domain string) string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return ""
	}
	if strings.Contains(domain, "://") {
		parsed, err := url.Parse(domain)
		if err != nil {
			return ""
		}
		if parsed.EscapedPath() != "" && parsed.EscapedPath() != "/" ||
			parsed.RawQuery != "" ||
			parsed.Fragment != "" {
			return ""
		}
		domain = parsed.Hostname()
	}
	domain = strings.TrimPrefix(domain, ".")
	if strings.ContainsAny(domain, "/?#") {
		return ""
	}

	return domain
}

func snippet(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= snippetRuneCap {
		return text
	}

	return string(runes[:snippetRuneCap])
}

func (m *inclusionMode) UnmarshalJSON(raw []byte) error {
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if enabled {
			*m = "true"
		} else {
			*m = "false"
		}
		return nil
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("include_answer: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "false", "true", "basic", "advanced":
		*m = inclusionMode(strings.ToLower(strings.TrimSpace(value)))
		return nil
	default:
		return badRequest("unsupported include_answer")
	}
}

type requestError string

func badRequest(message string) error { return requestError(message) }

func isBadRequest(err error) bool {
	var target requestError
	return errors.As(err, &target)
}

func (e requestError) Error() string { return string(e) }
