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

type coldPrimarySearch struct {
	delay time.Duration
}

func (s coldPrimarySearch) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	timer := time.NewTimer(s.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return searchcore.Response{Request: req}, fmt.Errorf("cold primary: %w", ctx.Err())
	case <-timer.C:
	}

	return searchcore.Response{
		Request: req,
		Results: []searchcore.Result{{
			DocumentID: "local-gap",
			Title:      "Local gap",
			URL:        "https://local.example/gap",
			Source:     req.Source,
		}},
		TotalResults: 1,
	}, nil
}

func TestAlwaysWebFallbackPublishesColdPrimaryBesideWeb(t *testing.T) {
	previousExact := webFallbackExactStageBudget
	previousParallel := webFallbackParallelExactStageBudget
	previousRecovery := localExactRecoveryBudget
	webFallbackExactStageBudget = 20 * time.Millisecond
	webFallbackParallelExactStageBudget = 120 * time.Millisecond
	localExactRecoveryBudget = 15 * time.Millisecond
	t.Cleanup(func() {
		webFallbackExactStageBudget = previousExact
		webFallbackParallelExactStageBudget = previousParallel
		localExactRecoveryBudget = previousRecovery
	})

	client := &http.Client{Transport: fallbackRoundTrip(func(request *http.Request) (
		*http.Response,
		error,
	) {
		timer := time.NewTimer(55 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-request.Context().Done():
			return nil, fmt.Errorf("web provider: %w", request.Context().Err())
		case <-timer.C:
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(mojeekListFixture)),
			Header:     make(http.Header),
		}, nil
	})}
	searcher := assemblePublicSearcher(
		coldPrimarySearch{delay: 45 * time.Millisecond},
		productionShapeSwarmMiss{},
		publicSearchAssembly{
			client: client,
			webFallback: webFallbackConfig{
				Privacy:  webFallbackPrivacyAlways,
				Provider: webFallbackProviderDDGS,
				Backend:  "mojeek",
				Timeout:  time.Second,
			},
		},
	)

	started := time.Now()
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "gap", Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("elapsed = %s", elapsed)
	}
	sources := map[searchcore.Source]bool{}
	for _, result := range response.Results {
		sources[result.Source] = true
	}
	if !sources[searchcore.SourceGlobal] || !sources[searchcore.SourceWeb] {
		t.Fatalf("response = %#v", response)
	}
}
