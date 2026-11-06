package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yago-crawler/internal/httpfetch"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestRunServiceReportsNotModifiedThenRefetchesWhenUnrecorded(t *testing.T) {
	restoreAssemblySeams(t)
	serveViaSlowSource(t)
	origin := loopbackOrigin(t)
	newCrawlerHTTPPageFetcher = func(*http.Client, string, int64) *httpfetch.PageFetcher {
		return httpfetch.NewPageFetcher(&http.Client{Transport: crawlerRobotsRoundTrip(
			func(request *http.Request) (*http.Response, error) {
				if request.Header.Get("If-None-Match") != "" {
					return &http.Response{
						StatusCode: http.StatusNotModified,
						Body:       http.NoBody,
						Request:    request,
					}, nil
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"text/html"},
						"Etag":         []string{`"v2"`},
					},
					Body: io.NopCloser(
						strings.NewReader("<html><body>harbor tide tables</body></html>"),
					),
					Request: request,
				}, nil
			},
		)}, "", 0)
	}
	revision := yagocrawlcontract.DocumentRevision{
		ContentHash:          "content-hash",
		ExtractionGeneration: 1,
		FetchedAt:            time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
	}
	order := orderMessage(t, origin.URL+"/")
	decoded, err := yagocrawlcontract.UnmarshalCrawlOrder(order.GetOrderJson())
	if err != nil {
		t.Fatalf("decode order: %v", err)
	}
	decoded.Requests[0].ConditionalFetch = &yagocrawlcontract.ConditionalFetch{
		Validators: yagocrawlcontract.HTTPValidators{ETag: `"v1"`},
		Revision:   revision,
	}
	if order.OrderJson, err = yagocrawlcontract.MarshalCrawlOrder(decoded); err != nil {
		t.Fatalf("encode order: %v", err)
	}
	exchange := &fakeExchange{
		orders:   []*crawlrpc.CrawlOrderMessage{order},
		ingested: make(chan *crawlrpc.IngestBatchMessage, 8),
		progress: make(chan *crawlrpc.CrawlProgressReport, 32),
	}
	stubExchange(t, exchange)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runDone := make(chan error, 1)
	go func() {
		runDone <- runServiceWithMetrics(ctx, flowConfig(t, 1), htmlPageSource(nil), crawlermetrics.New())
	}()

	awaitFinishedReport(t, exchange.progress)
	cancel()
	<-runDone
	close(exchange.ingested)
	batches := make([]yagocrawlcontract.IngestBatch, 0, 2)
	for message := range exchange.ingested {
		batch, err := yagocrawlcontract.UnmarshalIngestBatch(message.GetBatchJson())
		if err != nil {
			t.Fatalf("decode batch: %v", err)
		}
		batches = append(batches, batch)
	}
	if len(batches) != 2 ||
		batches[0].NotModified == nil || batches[0].NotModified.Revision != revision ||
		batches[1].NotModified != nil || batches[1].Document.NormalizedURL == "" ||
		batches[1].ResponseValidators == nil || batches[1].ResponseValidators.ETag != `"v2"` {
		t.Fatalf(
			"ingested batches = %+v, want a not-modified observation then a full document",
			batches,
		)
	}
}
