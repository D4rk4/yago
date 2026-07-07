package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/formatparse"
	"github.com/D4rk4/yago/yagocrawler/internal/ingest"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/pageindex"
	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
	"github.com/D4rk4/yago/yagocrawler/internal/robots"
	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
)

type Frontier interface {
	Jobs() <-chan crawljob.CrawlJob
	Submit(ctx context.Context, work crawljob.CrawlJob, links crawljob.DiscoveredLinks)
	Done(work crawljob.CrawlJob, deliveryFailed bool)
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
	// insecure serves jobs whose profile opted into IgnoreTLSAuthority; nil
	// keeps every job on the verifying chain.
	insecure pagefetch.PageSource
	// direct and insecureDirect serve jobs whose profile set IgnoreRobots; nil
	// keeps robots.txt enforced for every job.
	direct         pagefetch.PageSource
	insecureDirect pagefetch.PageSource
	// loadFeedback hears each host's throttle signals and successes so the
	// politeness pace can back off and recover; nil discards the signals.
	loadFeedback HostLoadFeedback
	index        pageindex.IndexBuilder
	emitter      ingest.BatchEmitter
	observer     Observer
	tally        RunTally
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
		tally:    noopRunTally{},
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
				// Info, not debug: a rejected seed is the difference between
				// a working crawl and a silently empty run.
				slog.InfoContext(
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

// jobFetcher picks the fetch chain for a job along two profile axes: the
// TLS-authority-ignoring chain when the profile opted in, and the
// robots-skipping variant when the profile explicitly opted out of robots.txt.
// A variant that is not wired falls back to the safe (robots-obeying,
// certificate-verifying) default.
func (p *Pipeline) jobFetcher(job crawljob.CrawlJob) pagefetch.PageSource {
	if job.IgnoreTLSAuthority && p.insecure != nil {
		if job.IgnoreRobots && p.insecureDirect != nil {
			return p.insecureDirect
		}

		return p.insecure
	}
	if job.IgnoreRobots && p.direct != nil {
		return p.direct
	}

	return p.fetcher
}

// WithInsecureFetcher installs the fetch chain used by jobs whose crawl
// profile set IgnoreTLSAuthority. A nil source is ignored.
func WithInsecureFetcher(source pagefetch.PageSource) Option {
	return func(p *Pipeline) {
		if source != nil {
			p.insecure = source
		}
	}
}

// HostLoadFeedback receives per-fetch server-load outcomes: Throttled after a
// 429/503 (with the server's Retry-After wish, zero when absent) and Succeeded
// after a served page, so an adaptive pace can widen and narrow each host's
// delay. Implementations must not block.
type HostLoadFeedback interface {
	Throttled(rawURL string, retryAfter time.Duration, at time.Time)
	Succeeded(rawURL string, at time.Time)
}

// WithHostLoadFeedback installs the politeness feedback sink. A nil sink is
// ignored so the pipeline keeps discarding the signals.
func WithHostLoadFeedback(feedback HostLoadFeedback) Option {
	return func(p *Pipeline) {
		if feedback != nil {
			p.loadFeedback = feedback
		}
	}
}

// WithRobotsIgnoringFetchers installs the fetch chains used by jobs whose
// crawl profile set IgnoreRobots: the certificate-verifying variant and the
// TLS-authority-ignoring one. Nil sources are ignored, so an unwired variant
// keeps robots enforced.
func WithRobotsIgnoringFetchers(verifying, insecure pagefetch.PageSource) Option {
	return func(p *Pipeline) {
		if verifying != nil {
			p.direct = verifying
		}
		if insecure != nil {
			p.insecureDirect = insecure
		}
	}
}

// fetchJob runs the job through its fetch chain, accounting the outcome: a
// robots denial and a hard failure land in the run tally, a throttle signal
// and a served page feed the politeness pace.
func (p *Pipeline) fetchJob(
	ctx context.Context,
	job crawljob.CrawlJob,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	p.observer.FetchAttempted()
	if job.DisableBrowser {
		ctx = pagefetch.WithoutBrowserFallback(ctx)
	}
	fetched, err := p.jobFetcher(job).Fetch(ctx, target)
	if err != nil {
		// A rejected page (blocked target, bad status, wrong content type)
		// counts as a failed fetch: without a counter a run would finish
		// all-zero with no trace of why.
		if errors.Is(err, robots.ErrDisallowed) {
			p.tally.RobotsDenied(job.Provenance)
		} else {
			p.observer.FetchFailed()
			p.tally.Failed(job.Provenance)
		}
		if throttled, ok := pagefetch.AsThrottled(err); ok && p.loadFeedback != nil {
			p.loadFeedback.Throttled(job.URL, throttled.RetryAfter, time.Now())
		}

		return pagefetch.FetchedPage{}, fmt.Errorf("fetch: %w", err)
	}
	if p.loadFeedback != nil {
		p.loadFeedback.Succeeded(job.URL, time.Now())
	}

	return fetched, nil
}

func (p *Pipeline) process(ctx context.Context, job crawljob.CrawlJob) error {
	p.observer.JobStarted()
	deliveryFailed := false
	defer func() { p.frontier.Done(job, deliveryFailed) }()
	defer p.observer.JobFinished()
	slog.DebugContext(ctx, msgJobFetching,
		slog.String("url", job.URL),
		slog.Int("depth", job.Depth),
	)
	target, ok := weburl.ParseBase(job.URL)
	if !ok {
		return fmt.Errorf("parse url: %s", job.URL)
	}
	fetched, err := p.fetchJob(ctx, job, target)
	if err != nil {
		return err
	}
	if !formatparse.Accepts(fetched.URL.String(), fetched.ContentType, job.Formats) {
		p.observer.FetchFailed()
		p.tally.Failed(job.Provenance)

		return fmt.Errorf(
			"content type %q: %w",
			fetched.ContentType,
			pagefetch.ErrUnsupportedContentType,
		)
	}
	p.observer.FetchSucceeded(len(fetched.Body))
	p.tally.Fetched(job.Provenance)
	page, parsed := formatparse.Parse(
		fetched.URL.String(),
		fetched.ContentType,
		fetched.Body,
		job.Formats,
	)
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
	if !job.Index || !parsed {
		slog.DebugContext(ctx, msgPageNotIndexed, slog.String("url", page.URL))

		return nil
	}
	deliveryFailed, err = p.indexAndEmit(ctx, job, page, fetched.ContentType)

	return err
}

// indexAndEmit builds the page's index artifacts and delivers them to the ingest
// emitter, reporting deliveryFailed when the durable emit itself fails so the run
// can be naked for redelivery rather than acked with references lost in flight.
func (p *Pipeline) indexAndEmit(
	ctx context.Context,
	job crawljob.CrawlJob,
	page pageparse.ParsedPage,
	contentType string,
) (deliveryFailed bool, err error) {
	stats := pageparse.BuildPageStats(page)
	artifacts, err := p.index.Build(page, stats)
	if err != nil {
		return false, fmt.Errorf("index: %w", err)
	}
	artifacts.Document.ContentType = contentType
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
		return true, fmt.Errorf("emit: %w", err)
	}
	p.observer.IngestPublished()
	p.tally.Indexed(job.Provenance)

	return false, nil
}
