package crawltraversal

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlfrontier"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageadmission"
)

type crawl struct {
	config   Config
	fetch    crawlcapability.PageRetrieval
	extract  crawlcapability.DocumentExtraction
	recrawl  crawlcapability.RecrawlDecision
	outputs  []crawlcapability.PagePublication
	observer crawlcapability.RunProgress
	clock    crawlcapability.Clock
	delivery crawlcapability.DeliveredOrder
	frontier *crawlfrontier.Frontier
	counted  int
	inflight int
	fatal    error
}

func (r *Crawler) newCrawl(delivery crawlcapability.DeliveredOrder) *crawl {
	return &crawl{
		config:   r.config,
		fetch:    r.fetch,
		extract:  r.extract,
		recrawl:  r.recrawl,
		outputs:  r.outputs,
		observer: r.observer,
		clock:    r.clock,
		delivery: delivery,
	}
}

func (c *crawl) run(ctx context.Context) error {
	seeds := c.canonicalSeeds(ctx, c.delivery.Order.SeedURLs)
	admission, err := pageadmission.New(
		c.delivery.Order.Profile,
		seeds,
		c.config.FrontierCapacity,
	)
	if err != nil {
		return fmt.Errorf("build admission: %w", err)
	}
	c.frontier = crawlfrontier.New(admission)
	for _, seed := range seeds {
		c.frontier.Admit(seed, 0)
	}

	runCtx, cancel := context.WithCancel(ctx)

	dispatch := make(chan crawlfrontier.Entry)
	results := make(chan visitOutcome, c.config.FetchConcurrency)
	var fetchers sync.WaitGroup
	for range c.config.FetchConcurrency {
		fetchers.Add(1)
		go func() {
			defer fetchers.Done()
			for entry := range dispatch {
				results <- c.visit(runCtx, entry)
			}
		}()
	}

	var heartbeat sync.WaitGroup
	if c.config.OwnershipInterval > 0 {
		lease := OwnershipLease{
			extend:   c.delivery.ExtendOwnership,
			interval: c.config.OwnershipInterval,
			clock:    c.clock,
		}
		heartbeat.Add(1)
		go func() {
			defer heartbeat.Done()
			lease.Renew(runCtx)
		}()
	}

	err = c.schedule(runCtx, cancel, dispatch, results)

	close(dispatch)
	fetchers.Wait()
	cancel()
	heartbeat.Wait()
	return err
}

func (c *crawl) schedule(
	ctx context.Context,
	cancel context.CancelFunc,
	dispatch chan crawlfrontier.Entry,
	results chan visitOutcome,
) error {
	budget := c.config.RunPageBudget
	for {
		if c.fatal != nil {
			return c.drainInflight(results)
		}
		if c.counted >= budget && c.inflight == 0 {
			c.disposePendingOverBudget()
			return nil
		}
		if c.frontier.Drained() && c.inflight == 0 {
			return nil
		}

		var dispatchable chan crawlfrontier.Entry
		var next crawlfrontier.Entry
		if c.dispatchable(budget) {
			next, _ = c.frontier.Peek()
			dispatchable = dispatch
		} else if c.inflight == 0 {
			if err := c.awaitEarliestDue(ctx); err != nil {
				c.fatal = err
				cancel()
			}
			continue
		}

		select {
		case dispatchable <- next:
			c.frontier.Next()
			c.inflight++
		case r := <-results:
			c.recordVisit(r, cancel)
		case <-ctx.Done():
			c.fatal = contextError(ctx)
			cancel()
		}
	}
}

func (c *crawl) dispatchable(budget int) bool {
	if c.fatal != nil {
		return false
	}
	if c.counted+c.inflight >= budget {
		return false
	}
	return c.frontier.HasReady()
}

func (c *crawl) recordVisit(r visitOutcome, cancel context.CancelFunc) {
	c.inflight--
	if r.err != nil {
		if c.fatal == nil {
			c.fatal = r.err
		}
		cancel()
		return
	}
	if r.deferred {
		c.deferEntry(r.entry, r.deferFor)
		return
	}
	if r.transient {
		c.retryEntry(r.entry)
		return
	}
	if r.counted {
		c.counted++
	}
	for _, link := range r.candidates {
		c.frontier.Admit(link.url, link.depth)
	}
}

func (c *crawl) deferEntry(entry crawlfrontier.Entry, deferFor time.Duration) {
	if entry.Deferrals >= c.config.MaxDeferralsPerURL {
		c.observer.PageDisposed(crawlcapability.DisposalFetchFailed)
		return
	}
	c.observer.RefusalHonored(crawlcapability.RefusalDeferred)
	entry.Deferrals++
	entry.NotBefore = c.clock.Now().Add(deferFor)
	c.frontier.Defer(entry)
}

func (c *crawl) retryEntry(entry crawlfrontier.Entry) {
	entry.Attempts++
	entry.NotBefore = c.clock.Now().Add(c.retryDelay(entry.Attempts))
	c.frontier.Defer(entry)
}

func (c *crawl) awaitEarliestDue(ctx context.Context) error {
	due, ok := c.frontier.EarliestDue()
	if !ok {
		return nil
	}
	wait := due.Sub(c.clock.Now())
	if wait > 0 {
		if err := c.clock.Sleep(ctx, wait); err != nil {
			return fmt.Errorf("await earliest: %w", err)
		}
	}
	c.frontier.PromoteDue(c.clock.Now())
	return nil
}

func (c *crawl) drainInflight(results chan visitOutcome) error {
	for c.inflight > 0 {
		<-results
		c.inflight--
	}
	return c.fatal
}

func (c *crawl) disposePendingOverBudget() {
	c.observer.BudgetExhausted()
	for range c.frontier.DrainPending() {
		c.observer.PageDisposed(crawlcapability.DisposalBudgetTruncated)
	}
}
