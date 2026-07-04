package yagonode

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/extractfetch"
)

func TestBuildExtractFetcherDisabled(t *testing.T) {
	if buildExtractFetcher(nodeConfig{}, nil) != nil {
		t.Fatal("fetcher must be nil when fetch-on-extract is disabled")
	}
}

func TestBuildExtractFetcherEnabled(t *testing.T) {
	config := nodeConfig{
		ExtractFetch: extractFetchConfig{Enabled: true, Timeout: time.Second, MaxBytes: 1024},
	}
	if buildExtractFetcher(config, &http.Client{}) == nil {
		t.Fatal("fetcher must be built when fetch-on-extract is enabled")
	}
}

func TestExtractContentFetcherAdapts(t *testing.T) {
	body := `<html><head><title>Doc</title></head><body><p>Para.</p></body></html>`
	client := &http.Client{
		Transport: fallbackRoundTrip(func(*http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Content-Type", "text/html")

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     header,
			}, nil
		}),
	}
	adapter := extractContentFetcher{fetcher: extractfetch.New(client, 0, 0)}

	content, err := adapter.Fetch(context.Background(), "https://example.com/")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if content.Title != "Doc" || content.Text != "Para." {
		t.Fatalf("content = %#v", content)
	}
}

func TestLoadExtractFetchConfig(t *testing.T) {
	getenv := func(key string) string {
		if key == envExtractFetchEnabled {
			return "true"
		}

		return ""
	}
	cfg, err := loadExtractFetchConfig(getenv)
	if err != nil {
		t.Fatalf("loadExtractFetchConfig: %v", err)
	}
	if !cfg.Enabled || cfg.Timeout != defaultExtractFetchTimeout {
		t.Fatalf("cfg = %#v", cfg)
	}
}
