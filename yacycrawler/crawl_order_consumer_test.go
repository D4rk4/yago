package yacycrawler_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
)

func TestCrawlOrderRoundTripCarriesProvenanceAndHandle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write(
			[]byte(`<html lang="en"><title>Hi</title><body>words</body></html>`),
		); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	jobs := yacycrawler.NewJobQueue(8)
	ingest := yacycrawler.NewBoundedQueue[yacycrawler.IngestBatch](8)
	orders := yacycrawler.NewBoundedQueue[yacycrawlcontract.CrawlOrder](2)
	registry := yacycrawler.NewCrawlProfileRegistry()

	fetcher := yacycrawler.NewPageFetcher(
		server.Client(), yacycrawler.DefaultMaxBodyBytes, yacycrawler.DefaultUserAgent,
	)
	publisher := yacycrawler.NewIngestPublisher(ingest)
	frontier := yacycrawler.NewFrontier(jobs, jobs.Close, registry)
	pipeline := yacycrawler.NewPipeline(
		jobs,
		fetcher,
		publisher,
		frontier,
		yacycrawler.NewBotWallDetector(),
	)
	consumer := yacycrawler.NewCrawlOrderConsumer(orders, registry, frontier)
	node := newFakeNodeIngest(ingest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeDone := make(chan struct{})
	go func() { node.Run(ctx); close(nodeDone) }()
	workersDone := make(chan struct{})
	go func() { pipeline.RunWorkers(ctx, 2); close(workersDone) }()
	consumerDone := make(chan struct{})
	go func() { consumer.Run(ctx); close(consumerDone) }()

	cfg := yacycrawler.DefaultCrawlConfig()
	cfg.MaxDepth = 0
	token := []byte("remote-peer:abc123")
	order := defaultCrawlOrder(cfg, token, server.URL)

	if err := orders.Publish(ctx, order); err != nil {
		t.Fatalf("publish order: %v", err)
	}
	orders.Close()
	<-consumerDone
	<-workersDone
	ingest.Close()
	<-nodeDone

	batches := node.Batches()
	if len(batches) != 1 {
		t.Fatalf("got %d batches, want 1", len(batches))
	}
	batch := batches[0]
	if batch.ProfileHandle != order.Profile.Handle {
		t.Errorf("batch handle = %q, want %q", batch.ProfileHandle, order.Profile.Handle)
	}
	if !bytes.Equal(batch.Provenance, token) {
		t.Errorf("batch provenance = %q, want %q", batch.Provenance, token)
	}
}

func TestCrawlOrderQueueAppliesBackpressure(t *testing.T) {
	orders := yacycrawler.NewBoundedQueue[yacycrawlcontract.CrawlOrder](1)
	if err := orders.Publish(context.Background(), yacycrawlcontract.CrawlOrder{}); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := orders.Publish(ctx, yacycrawlcontract.CrawlOrder{}); err == nil {
		t.Error("expected blocked publish on saturated order queue, got nil error")
	}
}

func TestIngestQueueFansInFromMultipleCrawlers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write(
			[]byte(`<html lang="en"><title>Hi</title><body>words</body></html>`),
		); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	ingest := yacycrawler.NewBoundedQueue[yacycrawler.IngestBatch](16)
	node := newFakeNodeIngest(ingest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeDone := make(chan struct{})
	go func() { node.Run(ctx); close(nodeDone) }()

	const crawlers = 2
	done := make(chan struct{}, crawlers)
	for range crawlers {
		go func() {
			jobs := yacycrawler.NewJobQueue(8)
			registry := yacycrawler.NewCrawlProfileRegistry()
			fetcher := yacycrawler.NewPageFetcher(
				server.Client(), yacycrawler.DefaultMaxBodyBytes, yacycrawler.DefaultUserAgent,
			)
			publisher := yacycrawler.NewIngestPublisher(ingest)
			frontier := yacycrawler.NewFrontier(jobs, jobs.Close, registry)
			pipeline := yacycrawler.NewPipeline(
				jobs,
				fetcher,
				publisher,
				frontier,
				yacycrawler.NewBotWallDetector(),
			)

			workersDone := make(chan struct{})
			go func() { pipeline.RunWorkers(ctx, 1); close(workersDone) }()
			if err := seedCrawl(ctx, frontier, registry, 0, server.URL); err != nil {
				t.Errorf("seed: %v", err)
			}
			<-workersDone
			done <- struct{}{}
		}()
	}
	for range crawlers {
		<-done
	}
	ingest.Close()
	<-nodeDone

	if len(node.Batches()) != crawlers {
		t.Errorf("expected %d fanned-in batches, got %d", crawlers, len(node.Batches()))
	}
}
