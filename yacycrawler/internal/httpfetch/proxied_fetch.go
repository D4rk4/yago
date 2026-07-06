package httpfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

const (
	headerUserAgent   = "User-Agent"
	headerContentType = "Content-Type"
	headerRetryAfter  = "Retry-After"
	headerXRobotsTag  = "X-Robots-Tag"

	defaultDeferFor = time.Minute
)

type ProxiedFetch struct {
	client       *http.Client
	userAgent    string
	maxBodyBytes int64
	deadline     time.Duration
}

func New(
	proxyURL *url.URL,
	userAgent string,
	maxBodyBytes int64,
	deadline time.Duration,
) *ProxiedFetch {
	return &ProxiedFetch{
		client: &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		},
		userAgent:    userAgent,
		maxBodyBytes: maxBodyBytes,
		deadline:     deadline,
	}
}

func (f *ProxiedFetch) Fetch(
	ctx context.Context,
	rawURL string,
) (crawlcapability.FetchOutcome, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, f.deadline)
	defer cancel()

	request, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return crawlcapability.FetchOutcome{}, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set(headerUserAgent, f.userAgent)

	response, err := f.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return crawlcapability.FetchOutcome{}, fmt.Errorf("fetch %s: %w", rawURL, ctx.Err())
		}
		return crawlcapability.FetchOutcome{Status: crawlcapability.FetchTransient}, nil
	}
	defer func() { _ = response.Body.Close() }()

	return f.classify(response)
}

func (f *ProxiedFetch) classify(response *http.Response) (crawlcapability.FetchOutcome, error) {
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return f.fetched(response)
	case response.StatusCode == http.StatusTooManyRequests,
		response.StatusCode == http.StatusServiceUnavailable:
		return crawlcapability.FetchOutcome{
			Status:   crawlcapability.FetchDeferred,
			DeferFor: retryAfter(response.Header.Get(headerRetryAfter)),
		}, nil
	case response.StatusCode == http.StatusUnauthorized,
		response.StatusCode == http.StatusForbidden,
		response.StatusCode == http.StatusUnavailableForLegalReasons:
		return crawlcapability.FetchOutcome{Status: crawlcapability.FetchCeased}, nil
	case response.StatusCode >= 400 && response.StatusCode < 500:
		return crawlcapability.FetchOutcome{Status: crawlcapability.FetchNotAPage}, nil
	default:
		return crawlcapability.FetchOutcome{Status: crawlcapability.FetchTransient}, nil
	}
}

func (f *ProxiedFetch) fetched(response *http.Response) (crawlcapability.FetchOutcome, error) {
	body, read := readBody(response.Body, f.maxBodyBytes+1)
	if !read {
		return crawlcapability.FetchOutcome{Status: crawlcapability.FetchTransient}, nil
	}
	truncated := int64(len(body)) > f.maxBodyBytes
	if truncated {
		body = body[:f.maxBodyBytes]
	}
	noIndex, noFollow := robotsDirectives(response.Header.Values(headerXRobotsTag))
	return crawlcapability.FetchOutcome{
		Status:               crawlcapability.FetchSucceeded,
		FinalURL:             response.Request.URL.String(),
		ContentType:          response.Header.Get(headerContentType),
		Body:                 body,
		Truncated:            truncated,
		RefusesIndexing:      noIndex,
		RefusesLinkDiscovery: noFollow,
	}, nil
}

func readBody(source io.Reader, limit int64) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(source, limit))
	if err != nil {
		return nil, false
	}
	return body, true
}

func robotsDirectives(values []string) (noIndex, noFollow bool) {
	for _, value := range values {
		for _, directive := range strings.Split(value, ",") {
			switch strings.ToLower(strings.TrimSpace(directive)) {
			case "noindex":
				noIndex = true
			case "nofollow":
				noFollow = true
			case "none":
				noIndex = true
				noFollow = true
			}
		}
	}
	return noIndex, noFollow
}

func retryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return defaultDeferFor
	}
	if seconds, err := strconv.Atoi(header); err == nil {
		if seconds < 0 {
			return defaultDeferFor
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(header); err == nil {
		if wait := time.Until(when); wait > 0 {
			return wait
		}
		return 0
	}
	return defaultDeferFor
}
