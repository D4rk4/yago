package yacycrawler_test

import (
	"context"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/yacycrawler"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const pageBody = `<html lang="en"><head><title>Platypus</title></head>
<body><p>The platypus swims in rivers.</p></body></html>`

func TestPipelineEndToEndDeliversBatch(t *testing.T) {
	rawURL := "http://example.test/"

	jobs := yacycrawler.NewJobQueue(4)
	ingest := yacycrawler.NewBoundedQueue[yacycrawler.IngestBatch](4)
	fetcher := pageSourceFunc(
		func(_ context.Context, rawURL string) (yacycrawler.FetchedPage, error) {
			return yacycrawler.FetchedPage{
				URL:         rawURL,
				ContentType: "text/html",
				Body:        []byte(pageBody),
			}, nil
		},
	)
	publisher := yacycrawler.NewIngestPublisher(ingest)
	registry := yacycrawler.NewCrawlProfileRegistry()
	frontier := yacycrawler.NewFrontier(jobs, jobs.Close, registry)
	pipeline := yacycrawler.NewPipeline(
		jobs,
		fetcher,
		publisher,
		frontier,
		yacycrawler.NewBotWallDetector(),
	)
	node := newFakeNodeIngest(ingest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeDone := make(chan struct{})
	go func() {
		node.Run(ctx)
		close(nodeDone)
	}()
	workersDone := make(chan struct{})
	go func() {
		pipeline.RunWorkers(ctx, 2)
		close(workersDone)
	}()

	if err := seedCrawl(ctx, frontier, registry, 0, rawURL); err != nil {
		t.Fatalf("seed: %v", err)
	}
	<-workersDone
	ingest.Close()
	<-nodeDone

	batches := node.Batches()
	if len(batches) != 1 {
		t.Fatalf("got %d batches, want 1", len(batches))
	}
	batch := batches[0]
	if len(batch.Metadata) != 1 {
		t.Fatalf("got %d metadata rows, want 1", len(batch.Metadata))
	}

	want := yacymodel.WordHash("platypus")
	found := false
	for _, entry := range batch.Postings {
		if entry.WordHash == want {
			found = true
		}
	}
	if !found {
		t.Errorf("no posting for 'platypus' word hash %q", want)
	}
}

func TestPipelineDropsBotWall(t *testing.T) {
	rawURL := "http://example.test/"

	jobs := yacycrawler.NewJobQueue(4)
	ingest := yacycrawler.NewBoundedQueue[yacycrawler.IngestBatch](4)
	fetcher := pageSourceFunc(
		func(_ context.Context, rawURL string) (yacycrawler.FetchedPage, error) {
			return yacycrawler.FetchedPage{
				URL:         rawURL,
				ContentType: "text/html",
				Body:        []byte("<html><head><title>Just a moment...</title></head></html>"),
			}, nil
		},
	)
	publisher := yacycrawler.NewIngestPublisher(ingest)
	registry := yacycrawler.NewCrawlProfileRegistry()
	frontier := yacycrawler.NewFrontier(jobs, jobs.Close, registry)
	pipeline := yacycrawler.NewPipeline(
		jobs,
		fetcher,
		publisher,
		frontier,
		yacycrawler.NewBotWallDetector(),
	)
	node := newFakeNodeIngest(ingest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeDone := make(chan struct{})
	go func() {
		node.Run(ctx)
		close(nodeDone)
	}()
	workersDone := make(chan struct{})
	go func() {
		pipeline.RunWorkers(ctx, 2)
		close(workersDone)
	}()

	if err := seedCrawl(ctx, frontier, registry, 0, rawURL); err != nil {
		t.Fatalf("seed: %v", err)
	}
	<-workersDone
	ingest.Close()
	<-nodeDone

	if len(node.Batches()) != 0 {
		t.Errorf("bot wall page should not be ingested, got %d batches", len(node.Batches()))
	}
}
