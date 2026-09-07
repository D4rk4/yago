package yagonode

import (
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
	client := &http.Client{
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
	assembly := productionShapeSearchAssembly(client)
	assembly.webFallback.Backend = "mojeek"
	assembly.webFallback.CacheTTL = 0
	assembly.storage.searchIndex = index
	search := assemblePublicSearcher(
		newLocalRankingSearcher(index, nil, nil),
		productionShapeSwarmMiss{},
		assembly,
	)
	durations := make([]time.Duration, 0, 25)
	for range 5 {
		for _, size := range []int{0, 1, 99, 100, 101} {
			before := webCalls.Load()
			start := time.Now()
			response, err := search.Search(
				t.Context(),
				searchcore.Request{
					Query:  fmt.Sprintf("needle corpus%d", size),
					Source: searchcore.SourceGlobal,
					Limit:  5,
				},
			)
			durations = append(durations, time.Since(start))
			if err != nil || len(response.Results) == 0 ||
				(response.LostSourceFailures() == 0 && (webCalls.Load() > before) != (size < 100)) {
				t.Fatalf(
					"primary size=%d results=%d web calls=%d error=%v",
					size,
					len(response.Results),
					webCalls.Load()-before,
					err,
				)
			}
		}
	}
	slices.Sort(durations)
	t.Logf(
		"301 documents; 25 requests; web calls=%d; empty answers=0; p50=%s p95=%s p99=%s",
		webCalls.Load(),
		durations[12],
		durations[23],
		durations[24],
	)
}
