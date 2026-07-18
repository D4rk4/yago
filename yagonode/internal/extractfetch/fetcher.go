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
	defaultMaxBytes           int64 = 2 << 20
	maximumFetchResponseBytes int64 = 4 << 20
	fetchUserAgent                  = "Mozilla/5.0 (compatible; yago-extract/1.0; " +
		"+https://github.com/D4rk4/yago)"
)

type Content struct {
	Title  string
	Text   string
	Images []string
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
	maxBytes = min(maxBytes, maximumFetchResponseBytes)

	return &Fetcher{client: client, maxBytes: maxBytes, timeout: timeout}
}

func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (Content, error) {
	doc, err := f.fetchDocument(ctx, rawURL)
	if err != nil {
		return Content{}, err
	}

	content := extract(doc)
	content.Images = collectImages(doc, rawURL)

	return content, nil
}

// fetchDocument retrieves and parses the URL under the fetcher's bounds.
func (f *Fetcher) fetchDocument(ctx context.Context, rawURL string) (*html.Node, error) {
	if f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build extract request: %w", err)
	}
	req.Header.Set("User-Agent", fetchUserAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", rawURL, resp.StatusCode)
	}
	if !isHTML(resp.Header.Get("Content-Type")) {
		return nil, fmt.Errorf("fetch %s: unsupported content type", rawURL)
	}

	limited := &io.LimitedReader{R: resp.Body, N: f.maxBytes + 1}
	doc, err := html.Parse(limited)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rawURL, err)
	}
	if limited.N == 0 {
		return nil, fmt.Errorf("fetch %s: response exceeds %d bytes", rawURL, f.maxBytes)
	}

	return doc, nil
}

func isHTML(contentType string) bool {
	if contentType == "" {
		return true
	}
	media := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))

	return media == "text/html" || media == "application/xhtml+xml"
}
