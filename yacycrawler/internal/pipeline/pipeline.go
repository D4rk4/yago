package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/botwall"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlwork"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/ingest"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageparse"
)

type Frontier interface {
	Jobs() <-chan crawlwork.CrawlJob
	Submit(ctx context.Context, work crawlwork.CrawlJob, links []string)
	Done(work crawlwork.CrawlJob)
}

const (
	msgBotWallDropped = "bot wall page dropped"
	msgJobFetching    = "crawl job fetching"
	msgPageCrawled    = "crawl page crawled"
)

type Pipeline struct {
	frontier Frontier
	fetcher  pagefetch.PageSource
	botWall  botwall.BotWallScreen
	index    pageindex.IndexBuilder
	emitter  ingest.BatchEmitter
}

func NewPipeline(
	frontier Frontier,
	fetcher pagefetch.PageSource,
	botWall botwall.BotWallScreen,
	index pageindex.IndexBuilder,
	emitter ingest.BatchEmitter,
) *Pipeline {
	return &Pipeline{
		frontier: frontier,
		fetcher:  fetcher,
		botWall:  botWall,
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
			if err := p.process(ctx, job); err != nil {
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

func (p *Pipeline) process(ctx context.Context, job crawlwork.CrawlJob) error {
	defer p.frontier.Done(job)
	slog.DebugContext(ctx, msgJobFetching,
		slog.String("url", job.URL),
		slog.Int("depth", job.Depth),
	)
	fetched, err := p.fetcher.Fetch(ctx, job.URL)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if p.botWall.IsBotWall(fetched) {
		slog.WarnContext(ctx, msgBotWallDropped, slog.String("url", job.URL))
		return nil
	}
	page := pageparse.ParseHTML(fetched.URL, fetched.ContentType, fetched.Body)
	slog.DebugContext(ctx, msgPageCrawled,
		slog.String("url", page.URL),
		slog.Int("links", len(page.Links)),
	)
	resolved := crawlwork.CrawlJob{
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
	if err := p.emitter.Emit(ctx, artifacts.Postings, artifacts.Metadata, ingest.Envelope{
		SourceURL:     page.URL,
		Provenance:    job.Provenance,
		ProfileHandle: job.ProfileHandle,
	}); err != nil {
		return fmt.Errorf("emit: %w", err)
	}
	return nil
}
