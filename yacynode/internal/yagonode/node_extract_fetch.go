package yagonode

import (
	"context"
	"fmt"
	"net/http"

	"github.com/D4rk4/yago/yacynode/internal/extractfetch"
	"github.com/D4rk4/yago/yacynode/internal/tavilyapi"
)

// buildExtractFetcher returns a fetch-on-extract fetcher for the Tavily
// `/extract` endpoint when enabled; otherwise nil, so an uncached URL becomes a
// controlled failure with no outbound request. The fetcher uses the shared
// egress-guarded client, so it cannot reach private networks.
func buildExtractFetcher(config nodeConfig, client *http.Client) tavilyapi.ContentFetcher {
	if !config.ExtractFetch.Enabled {
		return nil
	}

	return extractContentFetcher{
		fetcher: extractfetch.New(
			client,
			config.ExtractFetch.Timeout,
			config.ExtractFetch.MaxBytes,
		),
	}
}

type extractContentFetcher struct {
	fetcher *extractfetch.Fetcher
}

func (f extractContentFetcher) Fetch(
	ctx context.Context,
	url string,
) (tavilyapi.FetchedContent, error) {
	content, err := f.fetcher.Fetch(ctx, url)
	if err != nil {
		return tavilyapi.FetchedContent{}, fmt.Errorf("fetch on extract: %w", err)
	}

	return tavilyapi.FetchedContent{Title: content.Title, Text: content.Text}, nil
}
