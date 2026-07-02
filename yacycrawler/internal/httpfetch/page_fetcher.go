package httpfetch

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
)

type PageFetcher struct {
	client    *http.Client
	userAgent string
	maxBytes  int64
}

func NewPageFetcher(
	client *http.Client,
	userAgent string,
	maxBytes int64,
) *PageFetcher {
	if client == nil {
		client = http.DefaultClient
	}
	return &PageFetcher{client: client, userAgent: userAgent, maxBytes: maxBytes}
}

func (f *PageFetcher) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("create request: %w", err)
	}
	if f.userAgent != "" {
		request.Header.Set("User-Agent", f.userAgent)
	}

	response, err := f.client.Do(request)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("http fetch: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return pagefetch.FetchedPage{}, fmt.Errorf(
			"status %d: %w",
			response.StatusCode,
			pagefetch.ErrPageRejected,
		)
	}

	body, err := readBody(response.Body, f.maxBytes)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("read body: %w", err)
	}
	contentType := responseContentType(response.Header.Get("Content-Type"), body)
	if !acceptedContentType(contentType) {
		return pagefetch.FetchedPage{}, fmt.Errorf(
			"content type %q: %w",
			contentType,
			pagefetch.ErrPageRejected,
		)
	}

	finalURL := target
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL
	}
	return pagefetch.FetchedPage{
		URL:         finalURL,
		ContentType: contentType,
		Body:        body,
	}, nil
}

func readBody(body io.Reader, maxBytes int64) ([]byte, error) {
	var (
		read []byte
		err  error
	)
	if maxBytes <= 0 {
		read, err = io.ReadAll(body)
	} else {
		read, err = io.ReadAll(io.LimitReader(body, maxBytes))
	}
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return read, nil
}

func responseContentType(header string, body []byte) string {
	if strings.TrimSpace(header) != "" {
		return header
	}
	if len(body) == 0 {
		return ""
	}
	return http.DetectContentType(body)
}

func acceptedContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		mediaType, _, _ = strings.Cut(value, ";")
	}
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "text/html", "application/xhtml+xml":
		return true
	default:
		return false
	}
}
