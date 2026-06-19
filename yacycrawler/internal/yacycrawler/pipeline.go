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
	Done()
}

type BotWallScreen interface {
	IsBotWall(page FetchedPage) bool
}

const msgBotWallDropped = "bot wall page dropped"

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
				slog.Warn("crawl job failed", "url", job.URL, "error", err)
			}
		}
	}
}

func (p *Pipeline) process(ctx context.Context, job CrawlJob) error {
	defer p.frontier.Done()
	fetched, err := p.fetcher.Fetch(ctx, job.URL)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if p.botWall.IsBotWall(fetched) {
		slog.Warn(msgBotWallDropped, "url", job.URL)
		return nil
	}
	page := ParseHTML(fetched.URL, fetched.ContentType, fetched.Body)
	resolved := CrawlJob{
		URL:           page.URL,
		Depth:         job.Depth,
		ProfileHandle: job.ProfileHandle,
		Provenance:    job.Provenance,
	}
	p.frontier.Submit(ctx, resolved, page.Links)
	if err := p.publisher.Publish(ctx, page, job.ProfileHandle, job.Provenance); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	return nil
}
