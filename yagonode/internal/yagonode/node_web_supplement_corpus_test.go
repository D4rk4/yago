package yagonode

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestSupplementationOnBoundedDocumentCorpus(t *testing.T) {
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	for _, size := range []int{1, 99, 100, 101} {
		for ordinal := range size {
			err := index.Index(t.Context(), documentstore.Document{
				NormalizedURL: fmt.Sprintf("https://primary.example/%d/%d", size, ordinal),
				Title: fmt.Sprintf(
					"Needle corpus%d",
					size,
				),
				ExtractedText: "Synthetic reference material",
				Language:      "en",
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	var webCalls atomic.Int32
	client := supplementationTestClient(&webCalls)
	assembly := productionShapeSearchAssembly(client)
	assembly.webFallback.Backend = "mojeek"
	assembly.webFallback.CacheTTL = 0
	assembly.storage.searchIndex = index
	search := assemblePublicSearcher(
		newLocalRankingSearcher(index, nil, nil),
		productionShapeSwarmMiss{},
		assembly,
	)
	streamContext := searchcore.WithSearchProgressObserver(
		t.Context(), func(searchcore.SearchProgress) {},
	)
	durations := make([]time.Duration, 0, 25)
	incompleteEmptyAnswers := 0
	for range 5 {
		for _, size := range []int{0, 1, 99, 100, 101} {
			elapsed, incomplete := verifyBoundedCorpusSearch(
				t,
				search,
				streamContext,
				size,
				&webCalls,
			)
			durations = append(durations, elapsed)
			if incomplete {
				incompleteEmptyAnswers++
			}
		}
	}
	slices.Sort(durations)
	t.Logf(
		"301 documents; 25 requests; web calls=%d; marked incomplete empty answers=%d; p50=%s p95=%s p99=%s",
		webCalls.Load(),
		incompleteEmptyAnswers,
		durations[12],
		durations[23],
		durations[24],
	)
}

func verifyBoundedCorpusSearch(
	t *testing.T,
	search searchcore.Searcher,
	ctx context.Context,
	size int,
	webCalls *atomic.Int32,
) (time.Duration, bool) {
	t.Helper()
	before := webCalls.Load()
	start := time.Now()
	response, err := search.Search(
		ctx,
		searchcore.Request{
			Query:  fmt.Sprintf("needle corpus%d", size),
			Source: searchcore.SourceGlobal,
			Limit:  5,
		},
	)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf(
			"primary size=%d results=%d web calls=%d error=%v",
			size,
			len(response.Results),
			webCalls.Load()-before,
			err,
		)
	}
	if !primaryResultsAvailableOrMarkedIncomplete(t, response, size) {
		return elapsed, true
	}
	if !supplementWebCallExpected(response, size, webCalls.Load() > before, elapsed) {
		t.Fatalf(
			"primary size=%d results=%d web calls=%d",
			size,
			len(response.Results),
			webCalls.Load()-before,
		)
	}
	return elapsed, false
}

func primaryResultsAvailableOrMarkedIncomplete(
	t *testing.T,
	response searchcore.Response,
	size int,
) bool {
	t.Helper()
	if len(response.Results) > 0 {
		return true
	}
	if response.LostSourceFailures() == 0 {
		t.Fatalf("primary size=%d produced an unmarked empty answer", size)
	}
	return false
}

func supplementWebCallExpected(
	response searchcore.Response,
	primarySize int,
	webCalled bool,
	elapsed time.Duration,
) bool {
	if response.LostSourceFailures() > 0 ||
		elapsed >= interactiveSearchBudget-interactiveSearchCancellationGrace {
		return true
	}
	if primarySize < 100 {
		return webCalled
	}
	primaryMissedDecision := elapsed >= webFallbackSupplementDecisionCeiling

	return !webCalled || response.LostSourceFailures() > 0 || primaryMissedDecision
}

func TestSupplementWebCallExpectationAllowsReportedIncompleteSearch(t *testing.T) {
	localFailure := searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
		Source: searchcore.PartialFailureSourceLocalSearch,
	}}}
	tests := []struct {
		name        string
		response    searchcore.Response
		primarySize int
		webCalled   bool
		elapsed     time.Duration
		want        bool
	}{
		{
			name: "small complete corpus requires web call",
			response: searchcore.Response{Results: []searchcore.Result{{
				URL: "https://local.example/result",
			}}},
			primarySize: 1,
			webCalled:   true,
			want:        true,
		},
		{
			name:        "small corpus can report local-source loss",
			response:    localFailure,
			primarySize: 1,
			want:        true,
		},
		{
			name:        "small corpus can exhaust interactive budget",
			primarySize: 1,
			elapsed:     interactiveSearchBudget,
			want:        true,
		},
		{
			name:        "small complete corpus without web call fails expectation",
			primarySize: 1,
			want:        false,
		},
		{
			name:        "large corpus needs no web call",
			primarySize: 100,
			want:        true,
		},
		{
			name:        "large corpus rejects unnecessary web call",
			primarySize: 100,
			webCalled:   true,
			want:        false,
		},
		{
			name:        "large corpus accepts web call after missed decision",
			primarySize: 100,
			webCalled:   true,
			elapsed:     webFallbackSupplementDecisionCeiling,
			want:        true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := supplementWebCallExpected(
				test.response,
				test.primarySize,
				test.webCalled,
				test.elapsed,
			)
			if got != test.want {
				t.Fatalf("supplementWebCallExpected() = %t, want %t", got, test.want)
			}
		})
	}
}

func supplementationTestClient(webCalls *atomic.Int32) *http.Client {
	return &http.Client{
		Transport: fallbackRoundTrip(func(request *http.Request) (*http.Response, error) {
			webCalls.Add(1)
			title := html.EscapeString(request.URL.Query().Get("q"))
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(
					strings.NewReader(
						`<ol><li><h2><a href="https://web.example/reference">` + title + `</a></h2><p>` + title + `</p></li></ol>`,
					),
				),
			}, nil
		}),
	}
}
