package httpfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagoegress"
)

var ErrResponseTooLarge = errors.New("crawler response exceeds the configured byte limit")

type Limits struct {
	RequestTimeout      time.Duration
	ConnectTimeout      time.Duration
	TLSHandshakeTimeout time.Duration
	HeaderTimeout       time.Duration
	MaximumRedirects    int
	MaximumResponseSize int64
	MaximumIdleHosts    int
	MaximumHostActive   int
	UserAgent           string
}

type PageFetcher struct {
	client              *http.Client
	maximumResponseSize int64
	maximumRedirects    *atomic.Int32
	userAgent           string
}

type PageResponse struct {
	RequestedURL string
	FinalURL     string
	StatusCode   int
	ContentType  string
	RetryAfter   string
	Body         []byte
	FetchedAt    time.Time
	Duration     time.Duration
}

func NewPageFetcher(limits Limits, guard yagoegress.Guard) (*PageFetcher, error) {
	if limits.RequestTimeout <= 0 || limits.ConnectTimeout <= 0 ||
		limits.TLSHandshakeTimeout <= 0 || limits.HeaderTimeout <= 0 ||
		limits.MaximumRedirects < 0 ||
		limits.MaximumRedirects > yagocrawlcontract.MaximumPageRedirects ||
		limits.MaximumResponseSize <= 0 ||
		limits.MaximumIdleHosts < 1 || limits.MaximumHostActive < 1 || limits.UserAgent == "" {
		return nil, errors.New("invalid crawler HTTP limits")
	}
	dialer := &net.Dialer{
		Timeout:   limits.ConnectTimeout,
		KeepAlive: 30 * time.Second,
		Control:   guard.DialControl,
	}
	maximumRedirects := &atomic.Int32{}
	maximumRedirects.Store(int32(limits.MaximumRedirects))
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          limits.MaximumIdleHosts,
		MaxIdleConnsPerHost:   limits.MaximumHostActive,
		MaxConnsPerHost:       limits.MaximumHostActive,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   limits.TLSHandshakeTimeout,
		ResponseHeaderTimeout: limits.HeaderTimeout,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   limits.RequestTimeout,
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) > int(maximumRedirects.Load()) {
				return errors.New("crawler redirect limit reached")
			}
			if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
				return errors.New("crawler redirect uses a forbidden scheme")
			}
			if request.URL.User != nil {
				return errors.New("crawler redirect contains user information")
			}
			return nil
		},
	}
	return &PageFetcher{
		client:              client,
		maximumResponseSize: limits.MaximumResponseSize,
		maximumRedirects:    maximumRedirects,
		userAgent:           limits.UserAgent,
	}, nil
}

func (fetcher *PageFetcher) SetMaximumRedirects(maximum int) bool {
	if fetcher.maximumRedirects == nil || maximum < 0 ||
		maximum > yagocrawlcontract.MaximumPageRedirects {
		return false
	}
	fetcher.maximumRedirects.Store(int32(maximum))
	return true
}

func (fetcher *PageFetcher) Fetch(ctx context.Context, rawURL string) (PageResponse, error) {
	canonicalURL, ok := yagocrawlcontract.CanonicalURL(rawURL)
	if !ok {
		return PageResponse{}, errors.New("crawler URL is invalid")
	}
	parsed, err := url.Parse(canonicalURL)
	if err != nil || parsed.User != nil {
		return PageResponse{}, errors.New("crawler URL contains invalid authority data")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, canonicalURL, nil)
	if err != nil {
		return PageResponse{}, fmt.Errorf("create crawl request: %w", err)
	}
	request.Header.Set("User-Agent", fetcher.userAgent)
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.5")
	started := time.Now()
	response, err := fetcher.client.Do(request)
	if err != nil {
		return PageResponse{}, fmt.Errorf("fetch page: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, fetcher.maximumResponseSize+1))
	if err != nil {
		return PageResponse{}, fmt.Errorf("read crawl response: %w", err)
	}
	if int64(len(body)) > fetcher.maximumResponseSize {
		return PageResponse{}, ErrResponseTooLarge
	}
	finalURL := response.Request.URL.String()
	if normalized, ok := yagocrawlcontract.CanonicalURL(finalURL); ok {
		finalURL = normalized
	}
	return PageResponse{
		RequestedURL: canonicalURL,
		FinalURL:     finalURL,
		StatusCode:   response.StatusCode,
		ContentType:  response.Header.Get("Content-Type"),
		RetryAfter:   response.Header.Get("Retry-After"),
		Body:         body,
		FetchedAt:    time.Now().UTC(),
		Duration:     time.Since(started),
	}, nil
}

func (fetcher *PageFetcher) CloseIdleConnections() {
	fetcher.client.CloseIdleConnections()
}
