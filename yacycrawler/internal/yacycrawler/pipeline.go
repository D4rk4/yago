package yacycrawler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

type PageSource interface {
	Fetch(ctx context.Context, rawURL string) (FetchedPage, error)
}

type LinkFrontier interface {
	Submit(ctx context.Context, work CrawlJob, links []string)
	Done(work CrawlJob)
}

type BotWallScreen interface {
	IsBotWall(page FetchedPage) bool
}

const (
	msgBotWallDropped = "bot wall page dropped"
	msgJobFetching    = "crawl job fetching"
	msgPageCrawled    = "crawl page crawled"
)

type Pipeline struct {
	jobs      JobSource
	fetcher   PageSource
	publisher *IngestPublisher
	frontier  LinkFrontier
	botWall   BotWallScreen
}

func NewPipeline(
	jobs JobSource,
	fetcher PageSource,
	publisher *IngestPublisher,
	frontier LinkFrontier,
	botWall BotWallScreen,
) *Pipeline {
	return &Pipeline{
		jobs:      jobs,
		fetcher:   fetcher,
		publisher: publisher,
		frontier:  frontier,
		botWall:   botWall,
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
		case job, ok := <-p.jobs.Jobs():
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

func (p *Pipeline) process(ctx context.Context, job CrawlJob) error {
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
	page := ParseHTML(fetched.URL, fetched.ContentType, fetched.Body)
	slog.DebugContext(ctx, msgPageCrawled,
		slog.String("url", page.URL),
		slog.Int("links", len(page.Links)),
	)
	resolved := CrawlJob{
		URL:           page.URL,
		Depth:         job.Depth,
		ProfileHandle: job.ProfileHandle,
		Provenance:    job.Provenance,
		RunID:         job.RunID,
	}
	p.frontier.Submit(ctx, resolved, page.Links)
	if err := p.publisher.Publish(ctx, page, job.ProfileHandle, job.Provenance); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	return nil
}
