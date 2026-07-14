package yagonode

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestAlwaysWebFallbackUsesParallelProviderWindow(t *testing.T) {
	client := &http.Client{Transport: fallbackRoundTrip(func(request *http.Request) (
		*http.Response,
		error,
	) {
		timer := time.NewTimer(1050 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-request.Context().Done():
			return nil, fmt.Errorf("provider wait: %w", request.Context().Err())
		case <-timer.C:
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(mojeekListFixture)),
			Header:     make(http.Header),
		}, nil
	})}
	primary := stubPrimarySearcher{resp: searchcore.Response{
		Results: []searchcore.Result{{
			Title: "Local gap", URL: "https://local.example/gap", Source: searchcore.SourceLocal,
		}},
		TotalResults: 1,
	}}
	search := withWebFallback(primary, publicSearchAssembly{
		client: client,
		webFallback: webFallbackConfig{
			Privacy: webFallbackPrivacyAlways, Provider: webFallbackProviderDDGS,
			Backend: "mojeek",
		},
	})

	response, err := search.Search(context.Background(), searchcore.Request{
		Query: "gap", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	foundWeb := false
	for _, result := range response.Results {
		foundWeb = foundWeb || result.Source == searchcore.SourceWeb
	}
	if !foundWeb {
		t.Fatalf(
			"parallel response = %#v failures = %#v",
			response.Results,
			response.PartialFailures,
		)
	}
}
