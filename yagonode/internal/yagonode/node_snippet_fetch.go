package yagonode

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/extractfetch"
	"github.com/D4rk4/yago/yagonode/internal/snippetfetch"
)

const (
	// snippetFetchTimeout bounds one result-page load; the enricher's own pass
	// budget caps the whole batch.
	snippetFetchTimeout = 2 * time.Second
	// snippetFetchMaxBytes bounds a result page's body read.
	snippetFetchMaxBytes = 1 << 20
)

// buildSnippetEnricher wires the peer-result snippet fetcher over the node's
// egress-guarded client, or nil when the operator disabled the feature.
func buildSnippetEnricher(config nodeConfig, client *http.Client) *snippetfetch.Enricher {
	if !config.PeerSnippetFetch {
		return nil
	}
	fetcher := extractfetch.New(client, snippetFetchTimeout, snippetFetchMaxBytes)

	return snippetfetch.NewEnricher(func(ctx context.Context, rawURL string) (string, error) {
		content, err := fetcher.Fetch(ctx, rawURL)
		if err != nil {
			return "", fmt.Errorf("fetch result page: %w", err)
		}

		return content.Text, nil
	})
}
