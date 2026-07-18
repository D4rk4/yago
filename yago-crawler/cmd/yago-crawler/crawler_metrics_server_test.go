package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
)

func TestStartCrawlerMetricsDisabledWhenAddrEmpty(t *testing.T) {
	closer, err := startCrawlerMetrics(context.Background(), "", crawlermetrics.New().Handler())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestStartCrawlerMetricsRejectsBadAddr(t *testing.T) {
	_, err := startCrawlerMetrics(
		context.Background(),
		"not-an-address",
		crawlermetrics.New().Handler(),
	)
	if err == nil {
		t.Fatal("expected listen error for a bad address")
	}
}

func TestStartCrawlerMetricsBindsListener(t *testing.T) {
	closer, err := startCrawlerMetrics(
		context.Background(),
		"127.0.0.1:0",
		crawlermetrics.New().Handler(),
	)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestServeCrawlerMetricsServesRegisteredSeries(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	metrics := crawlermetrics.New()
	metrics.FetchAttempted()
	closer := serveCrawlerMetrics(listener, metrics.Handler())
	defer func() { _ = closer.Close() }()

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://"+listener.Addr().String()+crawlerMetricsPath,
		nil,
	)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "yacy_crawler_fetches_total 1") {
		t.Fatalf("metrics body missing fetches series:\n%s", body)
	}
}
