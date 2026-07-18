package tavilyapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	PathExtract    = "/extract"
	maxExtractURLs = 20
)

type ExtractRequest struct {
	URLs            urlList  `json:"urls"`
	Query           string   `json:"query,omitempty"`
	ChunksPerSource *int     `json:"chunks_per_source,omitempty"`
	ExtractDepth    string   `json:"extract_depth,omitempty"`
	Format          string   `json:"format,omitempty"`
	IncludeImages   bool     `json:"include_images,omitempty"`
	IncludeFavicon  bool     `json:"include_favicon,omitempty"`
	Timeout         *float64 `json:"timeout,omitempty"`
	IncludeUsage    bool     `json:"include_usage,omitempty"`
}

type urlList []string

func (l *urlList) UnmarshalJSON(raw []byte) error {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		*l = urlList{single}

		return nil
	}

	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return badRequest("urls must be a URL string or an array of URL strings")
	}
	*l = many

	return nil
}

type ExtractResponse struct {
	Results       []ExtractResult  `json:"results"`
	FailedResults []ExtractFailure `json:"failed_results"`
	ResponseTime  float64          `json:"response_time"`
	Usage         *SearchUsage     `json:"usage,omitempty"`
	RequestID     string           `json:"request_id"`
}

type ExtractResult struct {
	URL           string   `json:"url"`
	RawContent    string   `json:"raw_content"`
	Images        []string `json:"images,omitempty"`
	Favicon       string   `json:"favicon,omitempty"`
	includeImages bool
}

type ExtractFailure struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

type FetchedContent struct {
	Title  string
	Text   string
	Images []string
}

// ContentFetcher fetches and extracts a URL that is not in the index. It is
// satisfied by an egress-guarded adapter so this package stays independent of
// the HTTP client and HTML parser; a nil fetcher means fetch-on-extract is off.
type ContentFetcher interface {
	Fetch(ctx context.Context, url string) (FetchedContent, error)
}

type extractEndpoint struct {
	documents    documentstore.DocumentDirectory
	access       SearchAccessPolicy
	fetcher      ContentFetcher
	now          func() time.Time
	workDuration time.Duration
}

func MountExtract(
	mux *http.ServeMux,
	documents documentstore.DocumentDirectory,
	access SearchAccessPolicy,
	fetcher ContentFetcher,
) {
	mux.Handle(PathExtract, NewExtractEndpointWithFetcher(documents, access, fetcher))
}

func NewExtractEndpoint(documents documentstore.DocumentDirectory) http.Handler {
	return NewExtractEndpointWithFetcher(documents, SearchAccessPolicy{}, nil)
}

func NewExtractEndpointWithAccess(
	documents documentstore.DocumentDirectory,
	access SearchAccessPolicy,
) http.Handler {
	return NewExtractEndpointWithFetcher(documents, access, nil)
}

func NewExtractEndpointWithFetcher(
	documents documentstore.DocumentDirectory,
	access SearchAccessPolicy,
	fetcher ContentFetcher,
) http.Handler {
	return extractEndpoint{
		documents:    documents,
		access:       access,
		fetcher:      fetcher,
		now:          time.Now,
		workDuration: maximumRawContentWorkDuration,
	}
}

func (e extractEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", id)

		return
	}
	if decision := e.access.authorize(r, ScopeRaw); decision != DecisionAllow {
		writeAuthDecision(w, decision, id)

		return
	}
	release, admitted := enterRawContentWork(w, id)
	if !admitted {
		return
	}
	defer release()
	ctx, cancel := rawContentWorkContext(r.Context(), e.workDuration)
	defer cancel()
	stopBodyClose := closeRequestBodyWhenDone(ctx, r.Body)
	defer stopBodyClose()
	r = r.WithContext(ctx)

	var req ExtractRequest
	if err := decodeJSONRequest(w, r, &req); err != nil {
		if isJSONRequestTooLarge(err) {
			writeError(
				w,
				http.StatusRequestEntityTooLarge,
				requestTooLargeErrorCode,
				requestTooLargeErrorMessage,
				id,
			)

			return
		}
		message := "invalid extract request"
		if isBadRequest(err) {
			message = err.Error()
		}
		writeError(w, http.StatusBadRequest, "invalid_extract_request", message, id)

		return
	}
	if err := validateExtractRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_extract_request", err.Error(), id)

		return
	}
	workContext, cancelRequest := rawContentWorkContext(
		r.Context(),
		requestTimeout(req.Timeout, defaultExtractTimeout(req.ExtractDepth)),
	)
	defer cancelRequest()

	resp, err := e.extractResponse(workContext, req, e.now(), id)
	if err != nil {
		status, code := rawContentResponseError(
			err,
			"extract_failed",
			"invalid_extract_request",
		)
		writeError(w, status, code, err.Error(), id)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (e extractEndpoint) extractResponse(
	ctx context.Context,
	req ExtractRequest,
	start time.Time,
	id string,
) (ExtractResponse, error) {
	budget := &rawContentBudget{
		retained: rawContentEnvelopeBytes + len(id) +
			len(req.URLs)*(rawContentExtractResultBytes+rawContentExtractFailureBytes),
		output: rawContentEnvelopeBytes + rawContentJSONStringBytes(id),
	}
	results := make([]ExtractResult, 0, len(req.URLs))
	failures := make([]ExtractFailure, 0, len(req.URLs))
	for _, raw := range req.URLs {
		result, failure, err := e.extractOne(ctx, req, raw, budget)
		if err != nil {
			return ExtractResponse{}, err
		}
		if failure != nil {
			failures = append(failures, *failure)

			continue
		}
		results = append(results, result)
	}

	return ExtractResponse{
		Results:       results,
		FailedResults: failures,
		ResponseTime:  e.now().Sub(start).Seconds(),
		Usage:         responseUsageEnabled(req.IncludeUsage),
		RequestID:     id,
	}, nil
}

func (e extractEndpoint) extractOne(
	ctx context.Context,
	req ExtractRequest,
	raw string,
	budget *rawContentBudget,
) (ExtractResult, *ExtractFailure, error) {
	normalized, ok := normalizeExtractURL(raw)
	if !ok {
		failure, err := retainExtractFailure(
			budget,
			raw,
			"url must be an absolute http or https URL",
		)

		return ExtractResult{}, failure, err
	}
	doc, found, err := e.lookup(ctx, normalized)
	if err != nil {
		return ExtractResult{}, nil, err
	}
	if found {
		return retainDocumentExtractResult(req, raw, doc, budget)
	}
	if e.fetcher == nil {
		failure, failureErr := retainExtractFailure(
			budget,
			raw,
			"url is not in the index and fetch-on-extract is disabled",
		)

		return ExtractResult{}, failure, failureErr
	}

	fetched, err := e.fetcher.Fetch(ctx, normalized)
	if err != nil {
		failure, failureErr := retainExtractFailure(budget, raw, "fetch-on-extract failed")

		return ExtractResult{}, failure, failureErr
	}

	return retainFetchedExtractResult(req, raw, fetched, budget)
}

func retainDocumentExtractResult(
	req ExtractRequest,
	requestedURL string,
	doc documentstore.Document,
	budget *rawContentBudget,
) (ExtractResult, *ExtractFailure, error) {
	raw := requestedExtractContent(req, doc.ExtractedText)
	if strings.TrimSpace(req.Query) == "" && requestedContentFormat(req.Format) == "markdown" {
		var ok bool
		raw, ok = boundedDocumentMarkdown(doc, maximumRawContentResponseBytes-budget.retained)
		if !ok {
			return rejectedExtractResult(budget, requestedURL)
		}
	}
	var images []string
	if req.IncludeImages {
		images = make([]string, 0, maxResultImages)
		for _, image := range doc.Images {
			if len(images) >= maxResultImages {
				break
			}
			if image.URL != "" {
				images = append(images, image.URL)
			}
		}
	}

	return retainExtractResult(budget, requestedURL, raw, images, req.IncludeFavicon)
}

func retainFetchedExtractResult(
	req ExtractRequest,
	requestedURL string,
	content FetchedContent,
	budget *rawContentBudget,
) (ExtractResult, *ExtractFailure, error) {
	raw := requestedExtractContent(req, content.Text)
	if strings.TrimSpace(req.Query) == "" && requestedContentFormat(req.Format) == "markdown" {
		var ok bool
		raw, ok = boundedFetchedMarkdown(content, maximumRawContentResponseBytes-budget.retained)
		if !ok {
			return rejectedExtractResult(budget, requestedURL)
		}
	}

	images := []string(nil)
	if req.IncludeImages {
		images = boundedImageURLs(content.Images)
	}

	return retainExtractResult(budget, requestedURL, raw, images, req.IncludeFavicon)
}

func retainExtractResult(
	budget *rawContentBudget,
	requestedURL string,
	raw string,
	images []string,
	includeFavicon bool,
) (ExtractResult, *ExtractFailure, error) {
	favicon := ""
	if includeFavicon {
		favicon = faviconURL(requestedURL)
	}
	retained := len(requestedURL) + len(raw) + len(favicon) +
		len(images)*rawContentStringHeaderBytes
	output := rawContentResultJSONBytes + rawContentJSONStringBytes(requestedURL) +
		rawContentJSONStringBytes(raw) + rawContentJSONStringBytes(favicon)
	for _, image := range images {
		retained += len(image)
		output += rawContentJSONStringBytes(image)
	}
	if !budget.reserve(retained, output) {
		return rejectedExtractResult(budget, requestedURL)
	}
	result := ExtractResult{
		URL:           strings.Clone(requestedURL),
		RawContent:    strings.Clone(raw),
		Favicon:       strings.Clone(favicon),
		includeImages: images != nil,
	}
	if len(images) > 0 {
		result.Images = make([]string, len(images))
		for index, image := range images {
			result.Images[index] = strings.Clone(image)
		}
	}

	return result, nil, nil
}

func rejectedExtractResult(
	budget *rawContentBudget,
	requestedURL string,
) (ExtractResult, *ExtractFailure, error) {
	failure, err := retainExtractFailure(
		budget,
		requestedURL,
		"extracted content exceeds response limit",
	)

	return ExtractResult{}, failure, err
}

func retainExtractFailure(
	budget *rawContentBudget,
	requestedURL string,
	message string,
) (*ExtractFailure, error) {
	if !budget.reserve(
		len(requestedURL)+len(message),
		rawContentResultJSONBytes+rawContentJSONStringBytes(requestedURL)+
			rawContentJSONStringBytes(message),
	) {
		return nil, errRawContentBudgetExceeded
	}

	return &ExtractFailure{URL: strings.Clone(requestedURL), Error: strings.Clone(message)}, nil
}

func (e extractEndpoint) lookup(
	ctx context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	if e.documents == nil {
		return documentstore.Document{}, false, nil
	}
	doc, found, err := e.documents.Document(ctx, normalizedURL)
	if err != nil {
		return documentstore.Document{}, false, fmt.Errorf("document lookup failed: %w", err)
	}

	return doc, found, nil
}

func validateExtractRequest(req ExtractRequest) error {
	if len(req.URLs) == 0 {
		return badRequest("urls is required")
	}
	if len(req.URLs) > maxExtractURLs {
		return badRequest("urls must contain at most 20 entries")
	}
	if err := validateExtractDepth(req.ExtractDepth); err != nil {
		return err
	}
	if err := validateRelevantChunks(req.Query, req.ChunksPerSource); err != nil {
		return err
	}
	if err := validateTimeout(req.Timeout, 1, 60); err != nil {
		return err
	}

	return validateExtractFormat(req.Format)
}

func validateExtractDepth(depth string) error {
	switch strings.ToLower(strings.TrimSpace(depth)) {
	case "", "basic", "advanced":
		return nil
	default:
		return badRequest("unsupported extract_depth")
	}
}

func validateExtractFormat(format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "markdown", "text":
		return nil
	default:
		return badRequest("unsupported format")
	}
}

func faviconURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}

	return parsed.Scheme + "://" + parsed.Host + "/favicon.ico"
}

func normalizeExtractURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	if parsed.Host == "" {
		return "", false
	}
	parsed.Fragment = ""

	return parsed.String(), true
}
