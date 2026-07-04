package tavilyapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	PathSearch        = "/search"
	requestIDHeader   = "X-Request-ID"
	defaultMaxResults = 5
	maxResultsCap     = 20
	snippetRuneCap    = 320
	tavilyDateLayout  = "2006-01-02"
	maxResultImages   = 5
	maxResponseImages = 20
)

type searchEndpoint struct {
	search    searchcore.Searcher
	documents documentstore.DocumentDirectory
	access    SearchAccessPolicy
	now       func() time.Time
}

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

type SearchResponse struct {
	Query          string            `json:"query"`
	Answer         *string           `json:"answer,omitempty"`
	Images         *[]SearchImage    `json:"images,omitempty"`
	Results        []SearchResult    `json:"results"`
	ResponseTime   float64           `json:"response_time"`
	AutoParameters map[string]string `json:"auto_parameters,omitempty"`
	Usage          *SearchUsage      `json:"usage,omitempty"`
	RequestID      string            `json:"request_id"`
}

type SearchResult struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	Content       string   `json:"content"`
	RawContent    *string  `json:"raw_content,omitempty"`
	Score         float64  `json:"score"`
	PublishedDate string   `json:"published_date,omitempty"`
	Favicon       string   `json:"favicon,omitempty"`
	Source        string   `json:"source,omitempty"`
	Images        []string `json:"images,omitempty"`
}

type SearchImage struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type SearchUsage struct {
	Credits int `json:"credits"`
}

type ErrorResponse struct {
	Error     ErrorBody `json:"error"`
	RequestID string    `json:"request_id"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var randomRead = rand.Read

func Mount(
	mux *http.ServeMux,
	search searchcore.Searcher,
	documents documentstore.DocumentDirectory,
	access SearchAccessPolicy,
) {
	mux.Handle(PathSearch, NewSearchEndpointWithAccess(search, documents, access))
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
	return searchEndpoint{
		search:    search,
		documents: documents,
		access:    access,
		now:       time.Now,
	}
}

func (e searchEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", id)
		return
	}

	var req SearchRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		message := "invalid search request"
		if isBadRequest(err) {
			message = err.Error()
		}
		writeError(w, http.StatusBadRequest, "invalid_search_request", message, id)
		return
	}
	if decision := e.access.authorize(r, searchScope(req)); decision != DecisionAllow {
		writeAuthDecision(w, decision, id)
		return
	}

	start := e.now()
	resp, err := e.searchResponse(r.Context(), req, start, id)
	if err != nil {
		status := http.StatusInternalServerError
		code := "search_failed"
		if isBadRequest(err) {
			status = http.StatusBadRequest
			code = "invalid_search_request"
		}
		writeError(w, status, code, err.Error(), id)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
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
	coreReq, err := coreRequest(req)
	if err != nil {
		return SearchResponse{}, err
	}
	if coreReq.Limit == 0 {
		return SearchResponse{
			Query:        strings.TrimSpace(req.Query),
			Results:      []SearchResult{},
			ResponseTime: e.now().Sub(start).Seconds(),
			Usage:        responseUsage(req),
			RequestID:    id,
		}, nil
	}

	resp, err := e.search.Search(ctx, coreReq)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("search failed: %w", err)
	}

	results, images, err := e.responseResults(ctx, req, coreReq, resp.Results)
	if err != nil {
		return SearchResponse{}, err
	}

	return SearchResponse{
		Query:          coreReq.Query,
		Answer:         responseAnswer(req),
		Images:         responseImages(req, images),
		Results:        results,
		ResponseTime:   e.now().Sub(start).Seconds(),
		AutoParameters: responseAutoParameters(req, coreReq),
		Usage:          responseUsage(req),
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
	if err := validateChunksPerSource(req.ChunksPerSource); err != nil {
		return err
	}
	if err := validateDateRange(req.StartDate, req.EndDate); err != nil {
		return err
	}
	if err := validateCountry(req.Country); err != nil {
		return err
	}
	for _, domain := range append(req.IncludeDomains, req.ExcludeDomains...) {
		if normalizeDomain(domain) == "" {
			return badRequest("domain filters must be host names")
		}
	}

	return nil
}

func validateChunksPerSource(value *int) error {
	if value == nil {
		return nil
	}
	if *value < 1 || *value > 3 {
		return badRequest("chunks_per_source must be between 1 and 3")
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

func validateCountry(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if len([]rune(value)) > 64 || strings.ContainsAny(value, "\r\n\t") {
		return badRequest("country must be a country name")
	}

	return nil
}

func (e searchEndpoint) responseResults(
	ctx context.Context,
	req SearchRequest,
	coreReq searchcore.Request,
	results []searchcore.Result,
) ([]SearchResult, []SearchImage, error) {
	out := make([]SearchResult, 0, len(results))
	images := make([]SearchImage, 0)
	for _, result := range results {
		if !allowsDomain(result, req.IncludeDomains, req.ExcludeDomains) {
			continue
		}

		item, itemImages, include, err := e.responseResult(ctx, req, coreReq, result)
		if err != nil {
			return nil, nil, err
		}
		if !include {
			continue
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
	var resultImages []string
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
			content = snippet(doc.ExtractedText)
			if req.IncludeRawContent.Enabled() {
				raw = &doc.ExtractedText
			}
		}
		resultImages, imageDetails = resultImagesFromDocument(req, doc)
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
		PublishedDate: result.Date,
		Favicon:       responseFavicon(req, result.URL),
		Source:        string(coreReq.Source),
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

func responseAnswer(req SearchRequest) *string {
	if !req.IncludeAnswer.Enabled() {
		return nil
	}
	answer := ""

	return &answer
}

func responseImages(req SearchRequest, images []SearchImage) *[]SearchImage {
	if !req.IncludeImages {
		return nil
	}
	if images == nil {
		images = []SearchImage{}
	}

	return &images
}

func resultImagesFromDocument(
	req SearchRequest,
	doc documentstore.Document,
) ([]string, []SearchImage) {
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

	return urls, images
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

func responseAutoParameters(
	req SearchRequest,
	coreReq searchcore.Request,
) map[string]string {
	if !req.AutoParameters {
		return nil
	}
	topic := strings.ToLower(strings.TrimSpace(req.Topic))
	if topic == "" {
		topic = "general"
	}
	depth := strings.ToLower(strings.TrimSpace(req.SearchDepth))
	if depth == "" {
		depth = "basic"
	}

	return map[string]string{
		"topic":        topic,
		"search_depth": depth,
		"source":       string(coreReq.Source),
	}
}

func responseUsage(req SearchRequest) *SearchUsage {
	if !req.IncludeUsage {
		return nil
	}

	return &SearchUsage{Credits: 0}
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
			*m = "true"
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
	case "", "false", "true", "markdown", "text":
		*m = rawContentMode(strings.ToLower(strings.TrimSpace(value)))
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
	if id := strings.TrimSpace(r.Header.Get(requestIDHeader)); id != "" {
		return id
	}

	return generatedRequestID()
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

func writeError(w http.ResponseWriter, status int, code, message, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorBody{
			Code:    code,
			Message: message,
		},
		RequestID: requestID,
	})
}

type requestError string

func badRequest(message string) error { return requestError(message) }

func isBadRequest(err error) bool {
	var target requestError
	return errors.As(err, &target)
}

func (e requestError) Error() string { return string(e) }
