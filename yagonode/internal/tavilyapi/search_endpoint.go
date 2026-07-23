package tavilyapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	PathSearch            = "/search"
	requestIDHeader       = "X-Request-ID"
	defaultMaxResults     = 5
	maxResultsCap         = 20
	snippetRuneCap        = 320
	tavilyDateLayout      = "2006-01-02"
	maxResultImages       = 5
	maxResponseImages     = 20
	maximumRequestIDBytes = 128
)

type searchEndpoint struct {
	search          searchcore.Searcher
	documents       documentstore.DocumentDirectory
	access          SearchAccessPolicy
	admission       SearchAdmission
	intake          *requestAdmission
	now             func() time.Time
	rawWorkDuration time.Duration
}

type SearchAdmission func(*http.Request) (func(), int, time.Duration)

type SearchAccessPolicy struct {
	BearerToken string
	Authorizer  ScopeAuthorizer
}

type SearchRequest struct {
	Query                    string         `json:"query"`
	SearchDepth              string         `json:"search_depth,omitempty"`
	ChunksPerSource          *int           `json:"chunks_per_source,omitempty"`
	MaxResults               *int           `json:"max_results,omitempty"`
	Topic                    string         `json:"topic,omitempty"`
	TimeRange                string         `json:"time_range,omitempty"`
	Days                     *int           `json:"days,omitempty"`
	StartDate                string         `json:"start_date,omitempty"`
	EndDate                  string         `json:"end_date,omitempty"`
	IncludeAnswer            inclusionMode  `json:"include_answer,omitempty"`
	IncludeRawContent        rawContentMode `json:"include_raw_content,omitempty"`
	IncludeImages            bool           `json:"include_images,omitempty"`
	IncludeImageDescriptions bool           `json:"include_image_descriptions,omitempty"`
	IncludeFavicon           bool           `json:"include_favicon,omitempty"`
	IncludeDomains           []string       `json:"include_domains,omitempty"`
	ExcludeDomains           []string       `json:"exclude_domains,omitempty"`
	Country                  string         `json:"country,omitempty"`
	AutoParameters           bool           `json:"auto_parameters,omitempty"`
	ExactMatch               bool           `json:"exact_match,omitempty"`
	IncludeUsage             bool           `json:"include_usage,omitempty"`
	SafeSearch               bool           `json:"safe_search,omitempty"`
}

type inclusionMode string

// SearchResponse mirrors Tavily's /search payload shape: answer, images, and
// follow_up_questions are always present (null / [] when not requested), and
// images is an array of URL strings unless include_image_descriptions asks for
// {url, description} objects — real Tavily clients index into both.
type SearchResponse struct {
	Query             string            `json:"query"`
	FollowUpQuestions *[]string         `json:"follow_up_questions"`
	Answer            *string           `json:"answer"`
	Images            any               `json:"images"`
	Results           []SearchResult    `json:"results"`
	ResponseTime      float64           `json:"response_time"`
	AutoParameters    map[string]string `json:"auto_parameters,omitempty"`
	Usage             *SearchUsage      `json:"usage,omitempty"`
	RequestID         string            `json:"request_id"`
}

type SearchResult struct {
	Title         string  `json:"title"`
	URL           string  `json:"url"`
	Content       string  `json:"content"`
	RawContent    *string `json:"raw_content"`
	Score         float64 `json:"score"`
	PublishedDate string  `json:"published_date,omitempty"`
	Favicon       string  `json:"favicon,omitempty"`
	Images        any     `json:"images,omitempty"`
}

type SearchImage struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type SearchUsage struct {
	Credits int `json:"credits"`
}

type ErrorResponse struct {
	Detail ErrorDetail `json:"detail"`
}

// ErrorDetail is the Tavily-native error envelope body.
type ErrorDetail struct {
	Error string `json:"error"`
}

var randomRead = rand.Read

func Mount(
	mux *http.ServeMux,
	search searchcore.Searcher,
	documents documentstore.DocumentDirectory,
	access SearchAccessPolicy,
	admission SearchAdmission,
) {
	mux.Handle(PathSearch, newSearchEndpoint(search, documents, access, admission))
}

func NewSearchEndpoint(
	search searchcore.Searcher,
	documents documentstore.DocumentDirectory,
) http.Handler {
	return NewSearchEndpointWithAccess(search, documents, SearchAccessPolicy{})
}

func NewSearchEndpointWithAccess(
	search searchcore.Searcher,
	documents documentstore.DocumentDirectory,
	access SearchAccessPolicy,
) http.Handler {
	return newSearchEndpoint(search, documents, access, nil)
}

func newSearchEndpoint(
	search searchcore.Searcher,
	documents documentstore.DocumentDirectory,
	access SearchAccessPolicy,
	admission SearchAdmission,
) http.Handler {
	return searchEndpoint{
		search:          search,
		documents:       documents,
		access:          access,
		admission:       admission,
		intake:          newRequestAdmission(maximumConcurrentSearchRequestBodies),
		now:             time.Now,
		rawWorkDuration: maximumRawContentWorkDuration,
	}
}

func (e searchEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", id)
		return
	}
	credentialAuthorized := false
	if e.access.Authorizer == nil {
		if decision := e.access.authorize(r, ScopeRead); decision != DecisionAllow {
			writeAuthDecision(w, decision, id)

			return
		}
		credentialAuthorized = true
	} else if _, ok := bearerToken(r.Header.Get("Authorization")); !ok {
		writeAuthDecision(w, DecisionUnauthenticated, id)

		return
	}
	releaseIntake, admitted := enterSearchRequestIntake(w, id, e.intake)
	if !admitted {
		return
	}

	var req SearchRequest
	decodeErr := decodeJSONRequest(w, r, &req)
	releaseIntake()
	if decodeErr != nil {
		if isJSONRequestTooLarge(decodeErr) {
			writeError(
				w,
				http.StatusRequestEntityTooLarge,
				requestTooLargeErrorCode,
				requestTooLargeErrorMessage,
				id,
			)

			return
		}
		message := "invalid search request"
		if isBadRequest(decodeErr) {
			message = decodeErr.Error()
		}
		writeError(w, http.StatusBadRequest, "invalid_search_request", message, id)
		return
	}
	if !credentialAuthorized {
		if decision := e.access.authorize(r, searchScope(req)); decision != DecisionAllow {
			writeAuthDecision(w, decision, id)
			return
		}
	}
	callerContext := r.Context()
	r, releaseWork, admitted := e.enterWork(
		w,
		r,
		id,
		req.IncludeRawContent.Enabled(),
	)
	if !admitted {
		return
	}
	defer releaseWork()

	start := e.now()
	resp, err := e.searchResponseForCaller(
		r.Context(),
		callerContext,
		req,
		start,
		id,
	)
	writeSearchEndpointResponse(w, resp, err, id)
}

func searchScope(req SearchRequest) SearchScope {
	if req.IncludeRawContent.Enabled() {
		return ScopeRaw
	}

	return ScopeRead
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}

	return parts[1], true
}

func (e searchEndpoint) searchResponse(
	ctx context.Context,
	req SearchRequest,
	start time.Time,
	id string,
) (SearchResponse, error) {
	return e.searchResponseForCaller(ctx, ctx, req, start, id)
}

func (e searchEndpoint) searchResponseForCaller(
	ctx context.Context,
	callerContext context.Context,
	req SearchRequest,
	start time.Time,
	id string,
) (SearchResponse, error) {
	coreReq, err := coreRequest(req)
	if err != nil {
		return SearchResponse{}, err
	}
	if coreReq.Limit == 0 {
		results := []SearchResult{}
		return SearchResponse{
			Query:          coreReq.Query,
			Answer:         responseAnswer(req, results),
			Images:         responseImages(req, nil),
			Results:        results,
			ResponseTime:   e.now().Sub(start).Seconds(),
			AutoParameters: responseAutoParameters(req),
			Usage:          searchResponseUsage(req, false),
			RequestID:      id,
		}, nil
	}

	completion := searchCompletionFrom(e.search.Search(ctx, coreReq))
	if err := completion.errorForCaller(callerContext.Err()); err != nil {
		return SearchResponse{}, err
	}
	resp := completion.response
	dated := resp.Results
	if !coreReq.MinDate.IsZero() || !coreReq.MaxDate.IsZero() {
		// Remote and web results bypass the local index filter; hold them to
		// the same document-date bounds.
		dated = make([]searchcore.Result, 0, len(resp.Results))
		for _, result := range resp.Results {
			if resultWithinBounds(result.Date, coreReq.MinDate, coreReq.MaxDate) {
				dated = append(dated, result)
			}
		}
	}

	results, images, err := e.responseResults(ctx, req, coreReq, dated)
	if err != nil {
		return SearchResponse{}, err
	}
	if availabilityErr := searchAvailabilityError(
		len(results),
		len(resp.PartialFailures),
		callerContext.Err(),
	); availabilityErr != nil {
		return SearchResponse{}, availabilityErr
	}
	applyCanonicalRankScores(results)

	return SearchResponse{
		Query:          coreReq.Query,
		Answer:         responseAnswer(req, results),
		Images:         responseImages(req, images),
		Results:        results,
		ResponseTime:   e.now().Sub(start).Seconds(),
		AutoParameters: responseAutoParameters(req),
		Usage:          searchResponseUsage(req, true),
		RequestID:      id,
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
	parsed, err := searchcore.ParsePublicTextQuery(query)
	if err != nil {
		return searchcore.Request{}, badRequest(err.Error())
	}
	minDate, maxDate := requestTimeBounds(req)

	coreReq := searchcore.Request{
		Query:            query,
		Terms:            parsed.Terms,
		ExcludedTerms:    parsed.ExcludedTerms,
		Phrases:          parsed.Phrases(),
		Source:           source,
		Limit:            limit,
		ContentDomain:    searchcore.ContentDomainText,
		Language:         parsed.Language,
		SiteHost:         parsed.SiteHost,
		IncludeDomains:   normalizedDomains(req.IncludeDomains),
		ExcludeDomains:   normalizedDomains(req.ExcludeDomains),
		InURL:            parsed.InURL,
		TLD:              parsed.TLD,
		FileType:         parsed.FileType,
		Verify:           searchcore.VerifyFalse,
		SafeSearch:       req.SafeSearch,
		SortByDate:       parsed.SortByDate,
		Near:             parsed.Near,
		AllowWebFallback: true,
		MinDate:          minDate,
		MaxDate:          maxDate,
	}
	if normalizedSearchDepth(req.SearchDepth) == "advanced" {
		coreReq.Verify = searchcore.VerifyIfExist
	}
	if limit == 0 {
		coreReq.Limit = 0
	}
	if (len(coreReq.IncludeDomains) > 0 || len(coreReq.ExcludeDomains) > 0) &&
		coreReq.Limit > 0 {
		coreReq.Limit = min(
			coreReq.Limit*domainOverfetchFactor,
			maxResultsCap*domainOverfetchFactor,
		)
	}

	return coreReq, nil
}

const domainOverfetchFactor = 4

// documentContent picks the served content: advanced searches with
// chunks_per_source return the query-relevant chunks, everything else the
// leading snippet.
func documentContent(
	req SearchRequest,
	coreReq searchcore.Request,
	doc documentstore.Document,
) string {
	if req.ChunksPerSource != nil && *req.ChunksPerSource > 0 &&
		strings.EqualFold(strings.TrimSpace(req.SearchDepth), "advanced") {
		return relevantChunks(doc.ExtractedText, coreReq.Terms, *req.ChunksPerSource)
	}

	return snippet(doc.ExtractedText)
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
	switch normalizedSearchDepth(depth) {
	case "basic", "fast", "ultra-fast", "advanced":
		return searchcore.SourceGlobal, nil
	default:
		return "", badRequest(
			"Invalid search depth. Must be 'ultra-fast', 'fast', 'basic' or 'advanced'.",
		)
	}
}

func validateRequestOptions(req SearchRequest) error {
	if err := validateTopic(req.Topic); err != nil {
		return err
	}
	if req.Days != nil && *req.Days < 0 {
		return badRequest("days must not be negative")
	}
	if err := validateTimeRange(req.TimeRange); err != nil {
		return err
	}
	if err := validateChunksPerSource(req.SearchDepth, req.ChunksPerSource); err != nil {
		return err
	}
	if err := validateDateRange(req.StartDate, req.EndDate); err != nil {
		return err
	}
	if err := validateCountry(req.Country); err != nil {
		return err
	}
	if strings.TrimSpace(req.Country) != "" && normalizedTopic(req.Topic) != "general" {
		return badRequest("country is available only when topic is general")
	}
	if req.SafeSearch && unsupportedSafeSearchDepth(req.SearchDepth) {
		return badRequest("safe_search is not supported for fast or ultra-fast search depth")
	}
	if len(req.IncludeDomains) > maximumIncludeDomains {
		return badRequest("include_domains must contain at most 300 domains")
	}
	if len(req.ExcludeDomains) > maximumExcludeDomains {
		return badRequest("exclude_domains must contain at most 150 domains")
	}
	for _, domain := range append(req.IncludeDomains, req.ExcludeDomains...) {
		if normalizeDomain(domain) == "" {
			return badRequest("domain filters must be host names")
		}
	}

	return nil
}

func validateChunksPerSource(depth string, value *int) error {
	if value == nil {
		return nil
	}
	if normalizedSearchDepth(depth) != "advanced" {
		return badRequest("chunks_per_source is available only when search_depth is advanced")
	}
	if *value < 1 || *value > 3 {
		return badRequest("chunks_per_source must be between 1 and 3")
	}

	return nil
}

func validateTopic(topic string) error {
	switch normalizedTopic(topic) {
	case "", "general", "news", "finance":
		return nil
	default:
		return badRequest("unsupported topic")
	}
}

func normalizedTopic(topic string) string {
	normalized := strings.ToLower(strings.TrimSpace(topic))
	if normalized == "" {
		return "general"
	}

	return normalized
}

func validateTimeRange(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "day", "d", "week", "w", "month", "m", "year", "y":
		return nil
	default:
		return badRequest("unsupported time_range")
	}
}

func validateDateRange(start, end string) error {
	startTime, err := parseOptionalDate(start, "start_date")
	if err != nil {
		return err
	}
	endTime, err := parseOptionalDate(end, "end_date")
	if err != nil {
		return err
	}
	if !startTime.IsZero() && !endTime.IsZero() && startTime.After(endTime) {
		return badRequest("start_date must not be after end_date")
	}

	return nil
}

func parseOptionalDate(value, field string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(tavilyDateLayout, value)
	if err != nil {
		return time.Time{}, badRequest(field + " must use YYYY-MM-DD")
	}

	return parsed, nil
}

func (e searchEndpoint) responseResults(
	ctx context.Context,
	req SearchRequest,
	coreReq searchcore.Request,
	results []searchcore.Result,
) ([]SearchResult, []SearchImage, error) {
	limit, err := requestLimit(req.MaxResults)
	if err != nil {
		return nil, nil, err
	}
	var budget *rawContentBudget
	if req.IncludeRawContent.Enabled() {
		budget = newRawSearchResultBudget(limit)
	}
	out := make([]SearchResult, 0, limit)
	images := make([]SearchImage, 0, maxResponseImages)
	for _, result := range results {
		if len(out) >= limit {
			break
		}
		if !searchcore.ResultSatisfiesDomainConstraints(coreReq, result) {
			continue
		}

		item, itemImages, include, err := e.responseResult(ctx, req, coreReq, result)
		if err != nil {
			return nil, nil, err
		}
		if !include {
			continue
		}
		if budget != nil {
			item, itemImages, err = retainRawSearchResult(budget, item, itemImages)
			if err != nil {
				return nil, nil, err
			}
		}
		out = append(out, item)
		images = appendResponseImages(images, itemImages)
	}

	return out, images, nil
}

func (e searchEndpoint) responseResult(
	ctx context.Context,
	req SearchRequest,
	coreReq searchcore.Request,
	result searchcore.Result,
) (SearchResult, []SearchImage, bool, error) {
	content := result.Snippet
	var raw *string
	resultImages := emptySearchResultImages(req)
	var imageDetails []SearchImage
	doc, found, err := e.document(ctx, result.URL)
	if err != nil {
		return SearchResult{}, nil, false, err
	}
	if found {
		if doc.Title != "" {
			result.Title = doc.Title
		}
		if doc.ExtractedText != "" {
			content = documentContent(req, coreReq, doc)
			raw, err = rawDocumentResultContent(req, doc)
			if err != nil {
				return SearchResult{}, nil, false, err
			}
		}
		if !req.SafeSearch || result.SafetyRating == searchcore.SafetyGeneral {
			resultImages, imageDetails = resultImagesFromDocument(req, doc)
		}
	}
	if content == "" {
		content = result.Title
	}
	if req.ExactMatch && !matchesExactQuery(req.Query, result.Title, content, raw) {
		return SearchResult{}, nil, false, nil
	}

	return SearchResult{
		Title:         result.Title,
		URL:           result.URL,
		Content:       content,
		RawContent:    raw,
		Score:         result.Score,
		PublishedDate: responsePublishedDate(req, result.Date),
		Favicon:       responseFavicon(req, result.URL),
		Images:        resultImages,
	}, imageDetails, true, nil
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

func domainMatches(host, domain string) bool {
	domain = normalizeDomain(domain)
	if host == "" || domain == "" {
		return false
	}

	return host == domain || strings.HasSuffix(host, "."+domain)
}

func normalizedDomains(domains []string) []string {
	var normalized []string
	for _, domain := range domains {
		if value := normalizeDomain(domain); value != "" {
			normalized = append(normalized, value)
		}
	}

	return normalized
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
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return ""
		}
		domain = parsed.Hostname()
	} else if strings.Contains(domain, "/") {
		parts := strings.SplitN(domain, "/", 2)
		if parts[0] == "" || strings.ContainsAny(parts[1], "?#") {
			return ""
		}
		domain = parts[0]
	}
	domain = strings.TrimPrefix(domain, ".")
	domain = strings.TrimPrefix(domain, "*.")
	if strings.ContainsAny(domain, "/?#") {
		return ""
	}
	if address, err := netip.ParseAddr(strings.Trim(domain, "[]")); err == nil {
		return address.String()
	}

	return domain
}

func snippet(text string) string {
	var out strings.Builder
	out.Grow(min(len(text), snippetRuneCap*utf8.UTFMax))
	pendingSpace := false
	runes := 0
	for _, current := range text {
		if unicode.IsSpace(current) {
			pendingSpace = runes > 0

			continue
		}
		if pendingSpace && runes < snippetRuneCap {
			out.WriteByte(' ')
			runes++
		}
		if runes >= snippetRuneCap {
			break
		}
		out.WriteRune(current)
		runes++
		pendingSpace = false
	}

	return out.String()
}

// responseAnswer synthesizes the extractive answer from the served results.
func responseAnswer(req SearchRequest, results []SearchResult) *string {
	if !req.IncludeAnswer.Enabled() {
		return nil
	}
	answer := extractiveAnswer(req.IncludeAnswer, req.Query, results)

	return &answer
}

// responseImages renders the top-level images field in Tavily's dual shape:
// URL strings by default, {url, description} objects only when descriptions are
// requested, and an empty array when images were not requested at all.
func responseImages(req SearchRequest, images []SearchImage) any {
	if req.IncludeImages && req.IncludeImageDescriptions {
		if images == nil {
			images = []SearchImage{}
		}

		return images
	}
	urls := make([]string, 0, len(images))
	if !req.IncludeImages {
		return urls
	}
	for _, image := range images {
		urls = append(urls, image.URL)
	}

	return urls
}

func resultImagesFromDocument(
	req SearchRequest,
	doc documentstore.Document,
) (any, []SearchImage) {
	if !req.IncludeImages {
		return nil, nil
	}
	urls := make([]string, 0)
	images := make([]SearchImage, 0)
	for _, image := range doc.Images {
		if len(urls) >= maxResultImages {
			break
		}
		if image.URL == "" {
			continue
		}
		urls = append(urls, image.URL)
		item := SearchImage{URL: image.URL}
		if req.IncludeImageDescriptions {
			item.Description = image.AltText
		}
		images = append(images, item)
	}

	if req.IncludeImageDescriptions {
		return images, images
	}

	return urls, images
}

func emptySearchResultImages(req SearchRequest) any {
	if !req.IncludeImages {
		return nil
	}
	if req.IncludeImageDescriptions {
		return []SearchImage{}
	}

	return []string{}
}

func appendResponseImages(out, in []SearchImage) []SearchImage {
	for _, image := range in {
		if len(out) >= maxResponseImages {
			return out
		}
		out = append(out, image)
	}

	return out
}

func responseAutoParameters(req SearchRequest) map[string]string {
	if !req.AutoParameters {
		return nil
	}
	topic := strings.ToLower(strings.TrimSpace(req.Topic))
	if topic == "" {
		topic = "general"
	}
	depth := normalizedSearchDepth(req.SearchDepth)

	return map[string]string{
		"topic":        topic,
		"search_depth": depth,
	}
}

func responseFavicon(req SearchRequest, rawURL string) string {
	if !req.IncludeFavicon {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}

	return parsed.Scheme + "://" + parsed.Host + "/favicon.ico"
}

func matchesExactQuery(query, title, content string, raw *string) bool {
	haystack := strings.ToLower(title + " " + content + " " + rawContent(raw))
	for _, phrase := range exactNeedles(query) {
		if !strings.Contains(haystack, strings.ToLower(phrase)) {
			return false
		}
	}

	return true
}

func rawContent(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func exactNeedles(query string) []string {
	quoted := quotedPhrases(query)
	if len(quoted) > 0 {
		return quoted
	}
	query = strings.Join(strings.Fields(query), " ")
	if query == "" {
		return nil
	}

	return []string{query}
}

func quotedPhrases(query string) []string {
	var (
		out    []string
		token  strings.Builder
		quoted bool
	)
	for _, r := range query {
		switch {
		case r == '"' && quoted:
			value := strings.TrimSpace(token.String())
			token.Reset()
			quoted = false
			if value != "" {
				out = append(out, value)
			}
		case r == '"':
			token.Reset()
			quoted = true
		case quoted:
			token.WriteRune(r)
		}
	}

	return out
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

func (m inclusionMode) Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(string(m))) {
	case "true", "basic", "advanced":
		return true
	default:
		return false
	}
}

type rawContentMode string

func (m *rawContentMode) UnmarshalJSON(raw []byte) error {
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if enabled {
			*m = "markdown"
		} else {
			*m = "false"
		}
		return nil
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("include_raw_content: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "false", "markdown", "text":
		*m = rawContentMode(strings.ToLower(strings.TrimSpace(value)))
		return nil
	case "true":
		*m = "markdown"
		return nil
	default:
		return badRequest("unsupported include_raw_content")
	}
}

func (m rawContentMode) Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(string(m))) {
	case "true", "markdown", "text":
		return true
	default:
		return false
	}
}

func requestID(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get(requestIDHeader)); validRequestID(id) {
		return strings.Clone(id)
	}

	return generatedRequestID()
}

func validRequestID(id string) bool {
	return id != "" && len(id) <= maximumRequestIDBytes
}

func generatedRequestID() string {
	var value [16]byte
	if _, err := randomRead(value[:]); err != nil {
		return fmt.Sprintf("local-%d", time.Now().UnixNano())
	}

	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	)
}

func writeError(w http.ResponseWriter, status int, _ string, message, _ string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Detail: ErrorDetail{Error: message},
	})
}

type requestError string

func badRequest(message string) error { return requestError(message) }

func isBadRequest(err error) bool {
	var target requestError
	return errors.As(err, &target)
}

func (e requestError) Error() string { return string(e) }
