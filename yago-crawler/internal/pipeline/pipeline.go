package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/formatparse"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yago-crawler/internal/robots"
	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type Frontier interface {
	Take(context.Context) (crawljob.CrawlJob, bool)
	Submit(ctx context.Context, work crawljob.CrawlJob, links crawljob.DiscoveredLinks) uint64
	Done(work crawljob.CrawlJob, outcome yagocrawlcontract.CrawlRunTally)
	Abandon(work crawljob.CrawlJob)
	// ResolveRedirect checks a job's post-redirect final URL against the run's
	// visited-set, recording it when fresh; false means the target was already
	// processed this run and the page must be skipped.
	ResolveRedirect(job crawljob.CrawlJob, finalURL string) bool
}

const (
	msgPageRejected      = "crawl page rejected"
	msgCrawlJobFailed    = "crawl job failed"
	msgJobFetching       = "crawl job fetching"
	msgPageCrawled       = "crawl page crawled"
	msgPageNotIndexed    = "crawl page not indexed"
	msgPageNoindex       = "crawl page noindex"
	msgPageNofollow      = "crawl page nofollow"
	msgRedirectDuplicate = "crawl redirect target already visited"
	msgRedirectRejected  = "crawl redirect target rejected"
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
	leaseGrants  *crawllease.GrantRegistry
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
		leaseAvailabilityChanges := p.leaseAvailabilityChanges()
		leaseBindingChanges := p.leaseBindingChanges()
		job, ok := p.frontier.Take(acceptCtx)
		if !ok {
			return
		}
		jobContext, release, granted := p.grantedJobContext(fetchCtx, job)
		if !granted {
			p.frontier.Abandon(job)
			if !waitForLeaseAdmissionChange(
				acceptCtx,
				leaseAvailabilityChanges,
				leaseBindingChanges,
			) {
				return
			}

			continue
		}
		err := p.process(jobContext, job)
		release()
		if err != nil {
			if crawlJobLogLevel(err) == slog.LevelDebug {
				slog.DebugContext(fetchCtx, msgPageRejected,
					slog.String("url", job.URL),
					slog.Any("error", err),
				)
			} else {
				slog.WarnContext(fetchCtx, msgCrawlJobFailed,
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
	outcome *yagocrawlcontract.CrawlRunTally,
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
			outcome.RobotsDenied++
		} else {
			p.observer.FetchFailed()
			outcome.Failed++
		}
		p.recordHostFetchError(ctx, job, err)

		return pagefetch.FetchedPage{}, fmt.Errorf("fetch: %w", err)
	}
	return fetched, nil
}

func (p *Pipeline) process(ctx context.Context, job crawljob.CrawlJob) error {
	p.observer.JobStarted()
	outcome := yagocrawlcontract.CrawlRunTally{}
	abandoned := false
	defer func() {
		if abandoned {
			p.frontier.Abandon(job)

			return
		}
		p.frontier.Done(job, outcome)
	}()
	defer p.observer.JobFinished()
	slog.DebugContext(ctx, msgJobFetching,
		slog.String("url", job.URL),
		slog.Int("depth", job.Depth),
	)
	target, ok := weburl.ParseBase(job.URL)
	if !ok {
		outcome.Failed++

		return fmt.Errorf("parse url: %s", job.URL)
	}
	fetched, err := p.fetchJob(ctx, job, target, &outcome)
	if err != nil {
		removalErr := p.emitRemovalIfGone(ctx, job, err)
		abandoned = ctx.Err() != nil || errors.Is(removalErr, crawllease.ErrLeaseLost)
		if removalErr != nil {
			return errors.Join(err, removalErr)
		}

		return err
	}
	err = p.processFetchedPage(ctx, job, fetched, &outcome)
	abandoned = err != nil && (ctx.Err() != nil || errors.Is(err, crawllease.ErrLeaseLost))
	if err != nil && !abandoned {
		outcome.Failed++
	}

	return err
}

func (p *Pipeline) processFetchedPage(
	ctx context.Context,
	job crawljob.CrawlJob,
	fetched pagefetch.FetchedPage,
	outcome *yagocrawlcontract.CrawlRunTally,
) error {
	p.recordHostFetchSuccess(ctx, job)
	if !p.redirectAdmitted(ctx, job, fetched.URL.String()) {
		outcome.Duplicates++

		return nil
	}
	if !formatparse.Accepts(fetched.URL.String(), fetched.ContentType, job.Formats) {
		p.observer.FetchFailed()
		outcome.Failed++

		return fmt.Errorf(
			"content type %q: %w",
			fetched.ContentType,
			pagefetch.ErrUnsupportedContentType,
		)
	}
	p.observer.FetchSucceeded(len(fetched.Body))
	outcome.Fetched++
	page, parsed := formatparse.Parse(
		fetched.URL.String(),
		fetched.ContentType,
		fetched.Body,
		job.Formats,
	)
	page = pageWithSourceDate(page, fetched.LastModified, job.SourceModifiedAt)
	slog.DebugContext(ctx, msgPageCrawled,
		slog.String("url", page.URL),
		slog.Int("links", len(page.Links)),
	)
	directives := effectiveDirectives(job, page, fetched.RobotsTag)
	outcome.Duplicates += p.submitLinks(ctx, job, page, directives)
	if directives.noindex {
		slog.DebugContext(ctx, msgPageNoindex,
			slog.String("url", page.URL),
			slog.String("source", directives.noindexSource),
		)

		return nil
	}
	if !job.Index || !parsed {
		slog.DebugContext(ctx, msgPageNotIndexed, slog.String("url", page.URL))

		return nil
	}
	err := p.indexAndEmit(ctx, job, page, fetched.ContentType)
	if err == nil {
		outcome.Indexed++
	}

	return err
}

// redirectAdmitted applies the run's visited-set to a job's post-redirect
// final URL (CRAWL-30): when the fetch landed on a different URL than the job
// was dispatched for, the frontier checks and records the final URL, so two
// URLs redirecting to one target index it once per run. A duplicate target is
// counted by the frontier's duplicate tally and skipped entirely.
func (p *Pipeline) redirectAdmitted(ctx context.Context, job crawljob.CrawlJob, final string) bool {
	normFinal, ok := weburl.Normalize(final)
	if !ok {
		return true
	}
	if len(normFinal) > yagocrawlcontract.MaximumCrawlURLBytes {
		slog.DebugContext(ctx, msgRedirectRejected, slog.String("finalUrl", final))

		return false
	}
	_, ok = weburl.Normalize(job.URL)
	if !ok {
		return true
	}
	if p.frontier.ResolveRedirect(job, final) {
		return true
	}
	slog.DebugContext(ctx, msgRedirectDuplicate,
		slog.String("url", job.URL),
		slog.String("finalUrl", final),
	)

	return false
}

// submitLinks discovers the page's links into the frontier unless an
// effective page-level nofollow directive suppresses them (CRAWL-28).
func (p *Pipeline) submitLinks(
	ctx context.Context,
	job crawljob.CrawlJob,
	page pageparse.ParsedPage,
	directives pageDirectives,
) uint64 {
	if directives.nofollow {
		slog.DebugContext(ctx, msgPageNofollow,
			slog.String("url", page.URL),
			slog.String("source", directives.nofollowSource),
		)

		return 0
	}
	resolved := crawljob.CrawlJob{
		URL:           page.URL,
		Depth:         job.Depth,
		ProfileHandle: job.ProfileHandle,
		Provenance:    job.Provenance,
		RunID:         job.RunID,
		LeaseID:       job.LeaseID,
	}
	return p.frontier.Submit(ctx, resolved, crawljob.DiscoveredLinks{
		Followable: page.FollowableLinks,
		NoFollow:   page.NoFollowLinks,
	})
}

func (p *Pipeline) emitRemovalIfGone(ctx context.Context, job crawljob.CrawlJob, err error) error {
	var gone *pagefetch.GoneError
	if !errors.As(err, &gone) {
		return nil
	}
	if emitErr := p.emitter.EmitRemoval(
		ctx,
		ingest.Envelope{
			SourceURL:     job.URL,
			Provenance:    job.Provenance,
			ProfileHandle: job.ProfileHandle,
			ObservationID: job.ObservationID,
			ObservedAt:    job.ObservedAt,
		},
	); emitErr != nil {
		slog.WarnContext(ctx, "crawl page removal emit failed",
			slog.String("url", job.URL),
			slog.Any("error", emitErr))

		return fmt.Errorf("emit removal: %w", emitErr)
	}
	slog.DebugContext(ctx, "crawl dead page tombstoned",
		slog.String("url", job.URL),
		slog.Int("status", gone.Status))

	return nil
}

func (p *Pipeline) indexAndEmit(
	ctx context.Context,
	job crawljob.CrawlJob,
	page pageparse.ParsedPage,
	contentType string,
) error {
	stats := pageindex.BuildPageStats(page)
	artifacts, err := p.index.Build(page, stats)
	if err != nil {
		return fmt.Errorf("index: %w", err)
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
			ObservationID: job.ObservationID,
			ObservedAt:    job.ObservedAt,
		},
	); err != nil {
		return fmt.Errorf("emit: %w", err)
	}
	p.observer.IngestPublished()

	return nil
}
