package yagonode

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestSearchReplacesEmptyWebAttemptWhileExactSearchRuns(t *testing.T) {
	const rejected = `<div class="result"><a class="result__a" href="https://example.org/orchard">Orchard guide</a></div>`
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "rate limited", status: http.StatusTooManyRequests},
		{name: "no verified rows", status: http.StatusOK, body: rejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				assembly := productionShapeSearchAssembly(
					replacementEngineClient(test.status, test.body),
				)
				assembly.webFallback.Privacy = webFallbackPrivacyAlways
				assembly.webFallback.Backend = "duckduckgo"
				search := assemblePublicSearcher(
					webFallbackDeadlineProbe{},
					productionShapeSwarmMiss{},
					assembly,
				)
				response := runTavilySearch(t, search, "compass", "basic")
				if len(response.Results) != 1 ||
					response.Results[0].URL != "https://example.org/compass" {
					t.Fatalf("results = %+v", response.Results)
				}
			})
		})
	}
}

func replacementEngineClient(firstStatus int, firstBody string) *http.Client {
	return &http.Client{
		Transport: fallbackRoundTrip(func(request *http.Request) (*http.Response, error) {
			status, body := firstStatus, firstBody
			if request.URL.Host == "lite.duckduckgo.com" {
				select {
				case <-time.After(800 * time.Millisecond):
				case <-request.Context().Done():
					return nil, fmt.Errorf("web request: %w", request.Context().Err())
				}
				status = http.StatusOK
				body = `<a class="result-link" href="https://example.org/compass">Compass guide</a>`
			}
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	}
}
