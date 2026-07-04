package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/ingest"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/pageindex"
	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
)

type Frontier interface {
	Jobs() <-chan crawljob.CrawlJob
	Submit(ctx context.Context, work crawljob.CrawlJob, links crawljob.DiscoveredLinks)
	Done(work crawljob.CrawlJob)
}

const (
	msgPageRejected   = "crawl page rejected"
	msgJobFetching    = "crawl job fetching"
	msgPageCrawled    = "crawl page crawled"
	msgPageNotIndexed = "crawl page not indexed"
)

type Pipeline struct {
	frontier Frontier
	fetcher  pagefetch.PageSource
	index    pageindex.IndexBuilder
	emitter  ingest.BatchEmitter
	observer Observer
}

func NewPipeline(
	frontier Frontier,
	fetcher pagefetch.PageSource,
	index pageindex.IndexBuilder,
	emitter ingest.BatchEmitter,
	opts ...Option,
) *Pipeline {
	pipeline := &Pipeline{
		frontier: frontier,
		fetcher:  fetcher,
		index:    index,
		emitter:  emitter,
		observer: noopObserver{},
	}
	for _, opt := range opts {
		opt(pipeline)
	}

	return pipeline
}

// RunWorkers processes jobs until acceptCtx is cancelled or the frontier closes.
// acceptCtx governs whether a worker pulls the next job; fetchCtx governs the work
// already in flight, so cancelling acceptCtx alone lets current fetches finish
// before the worker stops, and cancelling fetchCtx aborts them.
func (p *Pipeline) RunWorkers(acceptCtx, fetchCtx context.Context, workers int) {
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			p.run(acceptCtx, fetchCtx)
		})
	}
	group.Wait()
}

func (p *Pipeline) run(acceptCtx, fetchCtx context.Context) {
	for {
		select {
		case <-acceptCtx.Done():
			return
		case job, ok := <-p.frontier.Jobs():
			if !ok {
				return
			}
			err := p.process(fetchCtx, job)
			switch {
			case err == nil:
			case errors.Is(err, pagefetch.ErrPageRejected):
				slog.DebugContext(
					fetchCtx,
					msgPageRejected,
					slog.String("url", job.URL),
					slog.Any("reason", err),
				)
			default:
				slog.WarnContext(
					fetchCtx,
					"crawl job failed",
					slog.String("url", job.URL),
					slog.Any("error", err),
				)
			}
		}
	}
}

func (p *Pipeline) process(ctx context.Context, job crawljob.CrawlJob) error {
	p.observer.JobStarted()
	defer p.frontier.Done(job)
	defer p.observer.JobFinished()
	slog.DebugContext(ctx, msgJobFetching,
		slog.String("url", job.URL),
		slog.Int("depth", job.Depth),
	)
	target, ok := weburl.ParseBase(job.URL)
	if !ok {
		return fmt.Errorf("parse url: %s", job.URL)
	}
	p.observer.FetchAttempted()
	fetched, err := p.fetcher.Fetch(ctx, target)
	if err != nil {
		if !errors.Is(err, pagefetch.ErrPageRejected) {
			p.observer.FetchFailed()
		}

		return fmt.Errorf("fetch: %w", err)
	}
	p.observer.FetchSucceeded(len(fetched.Body))
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
	p.frontier.Submit(ctx, resolved, crawljob.DiscoveredLinks{
		Followable: page.FollowableLinks,
		NoFollow:   page.NoFollowLinks,
	})
	if !job.Index {
		slog.DebugContext(ctx, msgPageNotIndexed, slog.String("url", page.URL))

		return nil
	}
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
	p.observer.IngestPublished()

	return nil
}
