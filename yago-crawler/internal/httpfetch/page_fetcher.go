package httpfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
)

type PageFetcher struct {
	client     *http.Client
	h1Client   *http.Client
	downgrades *hostDowngrades
	userAgent  string
	maxBytes   int64
}

func NewPageFetcher(
	client *http.Client,
	userAgent string,
	maxBytes int64,
) *PageFetcher {
	if client == nil {
		client = http.DefaultClient
	}
	return &PageFetcher{
		client:     client,
		downgrades: newHostDowngrades(),
		userAgent:  userAgent,
		maxBytes:   maxBytes,
	}
}

// WithHTTP1Fallback arms the h2-hostile-host fallback (CRAWL-18): on an
// HTTP/2 stream failure the request retries once through this h1-only
// client, and the host skips h2 for a while afterwards.
func (f *PageFetcher) WithHTTP1Fallback(client *http.Client) *PageFetcher {
	f.h1Client = client

	return f
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

	response, err := f.do(request, target)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("http fetch: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if pagefetch.ThrottledStatus(response.StatusCode) {
			return pagefetch.FetchedPage{}, &pagefetch.ThrottledError{
				Status: response.StatusCode,
				RetryAfter: pagefetch.ParseRetryAfter(
					response.Header.Get("Retry-After"),
					time.Now(),
				),
			}
		}
		if pagefetch.GoneStatus(response.StatusCode) {
			return pagefetch.FetchedPage{}, &pagefetch.GoneError{Status: response.StatusCode}
		}

		return pagefetch.FetchedPage{}, &pagefetch.HTTPStatusError{Status: response.StatusCode}
	}

	robotsTag := response.Header.Get("X-Robots-Tag")
	body, err := readBody(response.Body, f.maxBytes)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("read body: %w", err)
	}
	// Every content type leaves the fetcher: the format-parser registry
	// decides downstream what the job's toggles accept (CRAWL-17). The body
	// is already read at this point, so filtering here saved nothing and
	// silently cut every non-HTML format family off from the crawl.
	contentType := responseContentType(response.Header.Get("Content-Type"), body)

	finalURL := target
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL
	}
	return pagefetch.FetchedPage{
		URL:          finalURL,
		ContentType:  contentType,
		Body:         body,
		LastModified: responseLastModified(response.Header.Get("Last-Modified")),
		RobotsTag:    robotsTag,
	}, nil
}

func responseLastModified(value string) time.Time {
	parsed, err := http.ParseTime(value)
	if err != nil {
		return time.Time{}
	}

	return parsed.UTC()
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
	return http.DetectContentType(body)
}

// do routes one request through the right protocol: hosts remembered as
// h2-hostile go straight to the h1 client, everything else tries the primary
// client and falls back once on an HTTP/2 stream failure (CRAWL-18).
func (f *PageFetcher) do(request *http.Request, target *url.URL) (*http.Response, error) {
	if f.h1Client != nil && f.downgrades.Active(target.Hostname()) {
		return f.h1Client.Do(request) //nolint:wrapcheck // caller wraps uniformly.
	}
	response, err := f.client.Do(request)
	if err == nil || f.h1Client == nil || !IsHTTP2StreamError(err) {
		return response, err //nolint:wrapcheck // caller wraps uniformly.
	}
	f.downgrades.Mark(target.Hostname())
	retry := request.Clone(request.Context())

	return f.h1Client.Do(retry) //nolint:wrapcheck // caller wraps uniformly.
}
