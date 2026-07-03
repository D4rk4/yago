package tavilyapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/documentstore"
)

const (
	PathExtract    = "/extract"
	maxExtractURLs = 20
)

type ExtractRequest struct {
	URLs           urlList `json:"urls"`
	ExtractDepth   string  `json:"extract_depth,omitempty"`
	Format         string  `json:"format,omitempty"`
	IncludeImages  bool    `json:"include_images,omitempty"`
	IncludeFavicon bool    `json:"include_favicon,omitempty"`
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
	RequestID     string           `json:"request_id"`
}

type ExtractResult struct {
	URL        string   `json:"url"`
	RawContent string   `json:"raw_content"`
	Images     []string `json:"images,omitempty"`
	Favicon    string   `json:"favicon,omitempty"`
}

type ExtractFailure struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

type extractEndpoint struct {
	documents documentstore.DocumentDirectory
	access    SearchAccessPolicy
	now       func() time.Time
}

func MountExtract(
	mux *http.ServeMux,
	documents documentstore.DocumentDirectory,
	access SearchAccessPolicy,
) {
	mux.Handle(PathExtract, NewExtractEndpointWithAccess(documents, access))
}

func NewExtractEndpoint(documents documentstore.DocumentDirectory) http.Handler {
	return NewExtractEndpointWithAccess(documents, SearchAccessPolicy{})
}

func NewExtractEndpointWithAccess(
	documents documentstore.DocumentDirectory,
	access SearchAccessPolicy,
) http.Handler {
	return extractEndpoint{documents: documents, access: access, now: time.Now}
}

func (e extractEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", id)

		return
	}
	if !e.access.Allows(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(
			w,
			http.StatusUnauthorized,
			"unauthorized",
			"missing or invalid bearer token",
			id,
		)

		return
	}

	var req ExtractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		message := "invalid extract request"
		if isBadRequest(err) {
			message = err.Error()
		}
		writeError(w, http.StatusBadRequest, "invalid_extract_request", message, id)

		return
	}

	resp, err := e.extractResponse(r.Context(), req, e.now(), id)
	if err != nil {
		status := http.StatusInternalServerError
		code := "extract_failed"
		if isBadRequest(err) {
			status = http.StatusBadRequest
			code = "invalid_extract_request"
		}
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
	if err := validateExtractRequest(req); err != nil {
		return ExtractResponse{}, err
	}

	results := make([]ExtractResult, 0, len(req.URLs))
	failures := make([]ExtractFailure, 0)
	for _, raw := range req.URLs {
		normalized, ok := normalizeExtractURL(raw)
		if !ok {
			failures = append(failures, ExtractFailure{
				URL:   raw,
				Error: "url must be an absolute http or https URL",
			})

			continue
		}
		doc, found, err := e.lookup(ctx, normalized)
		if err != nil {
			return ExtractResponse{}, err
		}
		if !found {
			failures = append(failures, ExtractFailure{
				URL:   raw,
				Error: "url is not in the index and fetch-on-extract is disabled",
			})

			continue
		}
		results = append(results, extractResult(req, raw, doc))
	}

	return ExtractResponse{
		Results:       results,
		FailedResults: failures,
		ResponseTime:  e.now().Sub(start).Seconds(),
		RequestID:     id,
	}, nil
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

func extractResult(
	req ExtractRequest,
	requestedURL string,
	doc documentstore.Document,
) ExtractResult {
	result := ExtractResult{
		URL:        requestedURL,
		RawContent: extractContent(req.Format, doc),
	}
	if req.IncludeImages {
		result.Images = extractImages(doc)
	}
	if req.IncludeFavicon {
		result.Favicon = faviconURL(requestedURL)
	}

	return result
}

func extractContent(format string, doc documentstore.Document) string {
	if strings.EqualFold(strings.TrimSpace(format), "markdown") {
		return markdownContent(doc)
	}

	return doc.ExtractedText
}

func markdownContent(doc documentstore.Document) string {
	switch {
	case doc.Title == "":
		return doc.ExtractedText
	case doc.ExtractedText == "":
		return "# " + doc.Title
	default:
		return "# " + doc.Title + "\n\n" + doc.ExtractedText
	}
}

func extractImages(doc documentstore.Document) []string {
	urls := make([]string, 0)
	for _, image := range doc.Images {
		if len(urls) >= maxResultImages {
			break
		}
		if image.URL == "" {
			continue
		}
		urls = append(urls, image.URL)
	}
	if len(urls) == 0 {
		return nil
	}

	return urls
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
