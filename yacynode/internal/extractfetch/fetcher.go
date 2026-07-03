package extractfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	defaultMaxBytes int64 = 2 << 20
	fetchUserAgent        = "Mozilla/5.0 (compatible; yago-extract/1.0; " +
		"+https://github.com/D4rk4/yago)"
)

// Content is the text extracted from a fetched HTML page. Image extraction is
// intentionally omitted from the fetch-on-extract subset.
type Content struct {
	Title string
	Text  string
}

// Fetcher retrieves a URL through a caller-supplied HTTP client and extracts its
// title and visible text. The client is expected to be egress-guarded, so the
// fetch cannot reach private networks; this package adds an HTTP(S) response
// size bound and a per-request timeout on top of that guard.
type Fetcher struct {
	client   *http.Client
	maxBytes int64
	timeout  time.Duration
}

func New(client *http.Client, timeout time.Duration, maxBytes int64) *Fetcher {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}

	return &Fetcher{client: client, maxBytes: maxBytes, timeout: timeout}
}

func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (Content, error) {
	if f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Content{}, fmt.Errorf("build extract request: %w", err)
	}
	req.Header.Set("User-Agent", fetchUserAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return Content{}, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Content{}, fmt.Errorf("fetch %s: status %d", rawURL, resp.StatusCode)
	}
	if !isHTML(resp.Header.Get("Content-Type")) {
		return Content{}, fmt.Errorf("fetch %s: unsupported content type", rawURL)
	}

	doc, err := html.Parse(io.LimitReader(resp.Body, f.maxBytes))
	if err != nil {
		return Content{}, fmt.Errorf("parse %s: %w", rawURL, err)
	}

	return extract(doc), nil
}

func isHTML(contentType string) bool {
	if contentType == "" {
		return true
	}
	media := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))

	return media == "text/html" || media == "application/xhtml+xml"
}
