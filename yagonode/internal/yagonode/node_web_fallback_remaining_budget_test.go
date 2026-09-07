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

func TestQueuedSearchMissLeavesTimeForWebAnswer(t *testing.T) {
	client := &http.Client{
		Transport: fallbackRoundTrip(func(request *http.Request) (*http.Response, error) {
			timer := time.NewTimer(800 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-request.Context().Done():
				return nil, fmt.Errorf("provider: %w", request.Context().Err())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`<ol id="b_results"><li class="b_algo">
<h2><a href="https://example.org/needle">Needle term reference</a></h2>
<div class="b_caption"><p>Needle term reference material.</p></div></li></ol>`)),
			}, nil
		}),
	}
	assembly := productionShapeSearchAssembly(client)
	assembly.webFallback.Backend = "bing"
	search := assemblePublicSearcher(
		webFallbackDeadlineProbe{},
		productionShapeSwarmMiss{},
		assembly,
	)
	remaining := interactiveSearchBudget - interactiveSearchCancellationGrace - interactiveSearchAdmissionWait
	ctx, cancel := context.WithTimeout(t.Context(), remaining)
	defer cancel()
	response, err := search.Search(ctx, searchcore.Request{
		Query: "needle term", Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].Source != searchcore.SourceWeb {
		t.Fatalf(
			"results=%d failures=%+v error=%v",
			len(response.Results),
			response.PartialFailures,
			err,
		)
	}
}

func TestExactStageReservationPreservesUnconstrainedAndParallelBudgets(t *testing.T) {
	ceiling := webFallbackExactStageBudget
	reserve := webFallbackSequentialReserve(webFallbackConfig{Privacy: webFallbackPrivacyEnabled})
	for _, test := range []struct {
		name      string
		remaining time.Duration
		reserve   time.Duration
	}{
		{name: "unbounded", reserve: reserve},
		{name: "ample time", remaining: time.Hour, reserve: reserve},
		{name: "short caller deadline", remaining: time.Millisecond, reserve: reserve},
		{name: "parallel", remaining: time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.remaining > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, test.remaining)
				defer cancel()
			}
			if got := remainingExactStageBudget(ctx, ceiling, test.reserve); got != ceiling {
				t.Fatalf("budget=%s want=%s", got, ceiling)
			}
		})
	}
	if got := webFallbackSequentialReserve(
		webFallbackConfig{Privacy: webFallbackPrivacyAlways},
	); got != 0 {
		t.Fatalf("parallel reserve=%s", got)
	}
}
