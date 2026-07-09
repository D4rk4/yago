package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

func TestRunServiceIndexesCrawledPageIntoElasticsearch(t *testing.T) {
	var mu sync.Mutex
	var gotPath string
	elasticsearch := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			gotPath = r.URL.Path
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		}),
	)
	defer elasticsearch.Close()

	url := startNATS(t)
	cfg := ServiceConfig{
		NATSURL:            url,
		CrawledPageSubject: "yacy.crawl.pages",
		CrawledPageMaxMsgs: DefaultCrawledPageMaxMsgs,
		CrawledPageDurable: DefaultCrawledPageDurable,
		Concurrency:        DefaultConcurrency,
		SearchIndexEngine:  SearchIndexEngineElasticsearch,
		ElasticsearchURL:   elasticsearch.URL,
		ElasticsearchIndex: "yacy-text",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- RunService(ctx, cfg) }()

	js := connectJetStream(t, url)
	waitForCrawledPageStream(t, js)

	data, err := yacycrawlcontract.MarshalCrawledPage(yacycrawlcontract.CrawledPage{
		CanonicalURL: "https://example.com/",
		Title:        "Hi",
		Text:         "words here",
	})
	if err != nil {
		t.Fatalf("marshal crawled page: %v", err)
	}
	if _, err := js.Publish(ctx, cfg.CrawledPageSubject, data); err != nil {
		t.Fatalf("publish crawled page: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		path := gotPath
		mu.Unlock()
		if path == "/yacy-text/_doc/0f115db062b7c0dd030b16878c99dea5c354b49dc37b38eb8846179c7783e9d7" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	path := gotPath
	mu.Unlock()
	if path != "/yacy-text/_doc/0f115db062b7c0dd030b16878c99dea5c354b49dc37b38eb8846179c7783e9d7" {
		t.Fatalf("elasticsearch never received the indexed document, last path = %q", path)
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("service did not shut down after cancel")
	}
}

func waitForCrawledPageStream(t *testing.T, js jetstream.JetStream) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := js.Stream(
			context.Background(),
			yacycrawlcontract.CrawledPageStreamName,
		); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("stream %s not created in time", yacycrawlcontract.CrawledPageStreamName)
}
