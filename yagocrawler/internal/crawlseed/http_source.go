package crawlseed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
)

type HTTPSource struct {
	client    *http.Client
	userAgent string
	maxBytes  int64
}

func NewHTTPSource(client *http.Client, userAgent string, maxBytes int64) *HTTPSource {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPSource{client: client, userAgent: userAgent, maxBytes: maxBytes}
}

func (s *HTTPSource) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("create request: %w", err)
	}
	if s.userAgent != "" {
		request.Header.Set("User-Agent", s.userAgent)
	}

	response, err := s.client.Do(request)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("http fetch: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return pagefetch.FetchedPage{}, seedSourceStatusError(response)
	}

	body, err := sourceBody(response.Body, s.maxBytes)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("read body: %w", err)
	}

	finalURL := target
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL
	}
	return pagefetch.FetchedPage{
		URL:         finalURL,
		ContentType: sourceContentType(response.Header.Get("Content-Type"), body),
		Body:        body,
	}, nil
}

func sourceBody(body io.Reader, maxBytes int64) ([]byte, error) {
	reader := body
	if maxBytes > 0 {
		reader = io.LimitReader(body, maxBytes)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read source body: %w", err)
	}
	return data, nil
}

func sourceContentType(header string, body []byte) string {
	if strings.TrimSpace(header) != "" {
		return header
	}
	if len(body) == 0 {
		return ""
	}
	return http.DetectContentType(body)
}
