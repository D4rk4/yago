package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/D4rk4/yago/yacycrawler/internal/crawljob"
	"github.com/D4rk4/yago/yacycrawler/internal/ingest"
	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yacycrawler/internal/pageindex"
	"github.com/D4rk4/yago/yacycrawler/internal/pageparse"
	"github.com/D4rk4/yago/yacycrawler/internal/weburl"
)

type Frontier interface {
	Jobs() <-chan crawljob.CrawlJob
	Submit(ctx context.Context, work crawljob.CrawlJob, links []string)
	Done(work crawljob.CrawlJob)
}

const (
	msgPageRejected = "crawl page rejected"
	msgJobFetching  = "crawl job fetching"
	msgPageCrawled  = "crawl page crawled"
)

type Pipeline struct {
	frontier Frontier
	fetcher  pagefetch.PageSource
	index    pageindex.IndexBuilder
	emitter  ingest.BatchEmitter
}

func NewPipeline(
	frontier Frontier,
	fetcher pagefetch.PageSource,
	index pageindex.IndexBuilder,
	emitter ingest.BatchEmitter,
) *Pipeline {
	return &Pipeline{
		frontier: frontier,
		fetcher:  fetcher,
		index:    index,
		emitter:  emitter,
	}
}

func (p *Pipeline) RunWorkers(ctx context.Context, workers int) {
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			p.run(ctx)
		})
	}
	group.Wait()
}

func (p *Pipeline) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-p.frontier.Jobs():
			if !ok {
				return
			}
			err := p.process(ctx, job)
			switch {
			case err == nil:
			case errors.Is(err, pagefetch.ErrPageRejected):
				slog.DebugContext(
					ctx,
					msgPageRejected,
					slog.String("url", job.URL),
					slog.Any("reason", err),
				)
			default:
				slog.WarnContext(
					ctx,
					"crawl job failed",
					slog.String("url", job.URL),
					slog.Any("error", err),
				)
			}
		}
	}
}

func (p *Pipeline) process(ctx context.Context, job crawljob.CrawlJob) error {
	defer p.frontier.Done(job)
	slog.DebugContext(ctx, msgJobFetching,
		slog.String("url", job.URL),
		slog.Int("depth", job.Depth),
	)
	target, ok := weburl.ParseBase(job.URL)
	if !ok {
		return fmt.Errorf("parse url: %s", job.URL)
	}
	fetched, err := p.fetcher.Fetch(ctx, target)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	page := pageparse.ParseHTML(fetched.URL.String(), fetched.ContentType, fetched.Body)
	slog.DebugContext(ctx, msgPageCrawled,
		slog.String("url", page.URL),
		slog.Int("links", len(page.Links)),
	)
	resolved := crawljob.CrawlJob{
		URL:           page.URL,
		Depth:         job.Depth,
		ProfileHandle: job.ProfileHandle,
		Provenance:    job.Provenance,
		RunID:         job.RunID,
	}
	p.frontier.Submit(ctx, resolved, page.Links)
	stats := pageparse.BuildPageStats(page)
	artifacts, err := p.index.Build(page, stats)
	if err != nil {
		return fmt.Errorf("index: %w", err)
	}
	artifacts.Document.ContentType = fetched.ContentType
	artifacts.Document.FetchedAt = time.Now().UTC()
	if err := p.emitter.Emit(
		ctx,
		artifacts.Document,
		artifacts.Postings,
		artifacts.Metadata,
		ingest.Envelope{
			SourceURL:     page.URL,
			Provenance:    job.Provenance,
			ProfileHandle: job.ProfileHandle,
		},
	); err != nil {
		return fmt.Errorf("emit: %w", err)
	}
	return nil
}
