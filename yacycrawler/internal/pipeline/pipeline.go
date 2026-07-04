package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawledpage"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawledpageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawljob"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageparse"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/weburl"
)

type Frontier interface {
	Jobs() <-chan crawljob.CrawlJob
	Submit(ctx context.Context, work crawljob.CrawlJob, links []string)
	Done(work crawljob.CrawlJob, deliveryFailed bool)
}

const (
	msgPageRejected          = "crawl page rejected"
	msgJobFetching           = "crawl job fetching"
	msgPageCrawled           = "crawl page crawled"
	msgCrawledPageEmitFailed = "crawled page emit failed"
)

type Pipeline struct {
	frontier    Frontier
	fetcher     pagefetch.PageSource
	index       pageindex.IndexBuilder
	emitter     crawledpageindex.CrawledPageIndexEmitter
	pageEmitter crawledpage.CrawledPageEmitter
}

func NewPipeline(
	frontier Frontier,
	fetcher pagefetch.PageSource,
	index pageindex.IndexBuilder,
	emitter crawledpageindex.CrawledPageIndexEmitter,
	pageEmitter crawledpage.CrawledPageEmitter,
) *Pipeline {
	return &Pipeline{
		frontier:    frontier,
		fetcher:     fetcher,
		index:       index,
		emitter:     emitter,
		pageEmitter: pageEmitter,
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
	deliveryFailed := false
	defer func() { p.frontier.Done(job, deliveryFailed) }()
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
	if err := p.emitter.Emit(ctx, artifacts.Postings, artifacts.Metadata, crawledpageindex.Envelope{
		SourceURL:     page.URL,
		Provenance:    job.Provenance,
		ProfileHandle: job.ProfileHandle,
	}); err != nil {
		deliveryFailed = true
		return fmt.Errorf("emit: %w", err)
	}
	if err := p.pageEmitter.Emit(ctx, page, time.Now()); err != nil {
		slog.WarnContext(ctx, msgCrawledPageEmitFailed,
			slog.String("url", page.URL),
			slog.Any("error", err),
		)
	}
	return nil
}
