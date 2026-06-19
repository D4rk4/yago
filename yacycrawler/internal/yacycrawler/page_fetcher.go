package yacycrawler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

var (
	ErrUnsupportedContentType = errors.New("unsupported content type")
	ErrUnexpectedStatus       = errors.New("unexpected http status")
)

type PageFetcher struct {
	client    *http.Client
	maxBytes  int64
	userAgent string
}

func NewPageFetcher(client *http.Client, maxBytes int64, userAgent string) *PageFetcher {
	return &PageFetcher{client: client, maxBytes: maxBytes, userAgent: userAgent}
}

func (f *PageFetcher) Fetch(ctx context.Context, rawURL string) (FetchedPage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return FetchedPage{}, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("User-Agent", f.userAgent)
	response, err := f.client.Do(request)
	if err != nil {
		return FetchedPage{}, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer func() {
		if cerr := response.Body.Close(); cerr != nil {
			slog.Warn("response body close failed", "url", rawURL, "error", cerr)
		}
	}()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return FetchedPage{}, fmt.Errorf("%w: %d", ErrUnexpectedStatus, response.StatusCode)
	}
	contentType := response.Header.Get("Content-Type")
	if !isHTML(contentType) {
		return FetchedPage{}, fmt.Errorf("%w: %q", ErrUnsupportedContentType, contentType)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, f.maxBytes))
	if err != nil {
		return FetchedPage{}, fmt.Errorf("read body %s: %w", rawURL, err)
	}
	return FetchedPage{
		URL:         rawURL,
		ContentType: contentType,
		Body:        body,
	}, nil
}

func isHTML(contentType string) bool {
	if contentType == "" {
		return true
	}
	media := strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	return media == "text/html" || media == "application/xhtml+xml"
}
