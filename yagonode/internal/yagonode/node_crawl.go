package yagonode

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/crawlformats"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type crawlProcess interface {
	mountDispatch(mux *http.ServeMux)
	Run(ctx context.Context)
	Close()
}

type crawlRuntime struct {
	broker      *crawlbroker.CrawlBroker
	state       *vault.Vault
	ownsState   bool
	statePath   string
	consumer    *crawlresults.IngestConsumer
	runs        *crawlruns.Registry
	frontier    *recrawlfrontier.Frontier
	formats     *crawlformats.Store
	initiator   yagomodel.Hash
	pageBudget  *crawlRunPageBudget
	remoteCrawl remoteCrawlOrderObserver
}

const msgCrawlRuntimeStateCloseFailed = "crawl runtime state close failed"

var openCrawlBroker = crawlbroker.Open

func buildCrawlRuntime(
	ctx context.Context,
	config crawlConfig,
	identity nodeidentity.Identity,
	storage nodeStorage,
	storageVault *vault.Vault,
) (*crawlRuntime, error) {
	if !config.Enabled() {
		return nil, nil
	}
	coordination, err := openCrawlCoordinationRuntime(ctx, config, storageVault)
	if err != nil {
		return nil, err
	}
	ingest, err := openCrawlIngestRuntime(
		config,
		storage,
		storageVault,
		coordination.broker,
	)
	if err != nil {
		return nil, coordination.openFailure(err)
	}

	return &crawlRuntime{
		broker:     coordination.broker,
		state:      coordination.state,
		ownsState:  coordination.ownsState,
		statePath:  config.StatePath,
		consumer:   ingest.consumer,
		runs:       coordination.runs,
		frontier:   ingest.frontier,
		formats:    ingest.formats,
		initiator:  identity.Hash,
		pageBudget: newCrawlRunPageBudget(config.MaxPagesPerRun),
	}, nil
}

// dispatchQueue wraps the durable order queue so every dispatched or seeded crawl
// order records its profile in the recrawl frontier before it is enqueued.
func (r *crawlRuntime) dispatchQueue() crawldispatch.CrawlOrderQueue {
	// Format toggles are stamped first so the recorded recrawl profile carries
	// them too and re-dispatched URLs parse the same families.
	var queue crawldispatch.CrawlOrderQueue = profileRecordingQueue{
		inner:    formatStampingQueue{inner: r.broker.Orders, formats: r.formats},
		frontier: r.frontier,
	}
	if r.remoteCrawl != nil {
		queue = remoteCrawlObservedOrderQueue{inner: queue, observer: r.remoteCrawl}
	}

	return queue
}

func (r *crawlRuntime) useRemoteCrawlObserver(observer remoteCrawlOrderObserver) {
	r.remoteCrawl = observer
}

func (r *crawlRuntime) mountDispatch(mux *http.ServeMux) {
	crawldispatch.MountCrawlDispatch(
		mux,
		r.initiator,
		mintProvenance,
		r.dispatchQueue(),
		crawldispatch.WithMaxPagesPerRun(r.MaxPagesPerRun),
	)
}

func (r *crawlRuntime) observe(observer crawlresults.IngestObserver) {
	r.consumer.Observe(observer)
}

func (r *crawlRuntime) useContentSafetyClassifier(
	classifier crawlresults.ContentSafetyClassifier,
) {
	r.consumer.UseContentSafetyClassifier(classifier)
}

func attachContentSafetyClassifier(
	runtime crawlProcess,
	classifier crawlresults.ContentSafetyClassifier,
) {
	consumer, ok := runtime.(interface {
		useContentSafetyClassifier(crawlresults.ContentSafetyClassifier)
	})
	if ok {
		consumer.useContentSafetyClassifier(classifier)
	}
}

func (r *crawlRuntime) orderQueue() crawldispatch.CrawlOrderQueue {
	return r.dispatchQueue()
}

func (r *crawlRuntime) formatStore() *crawlformats.Store {
	return r.formats
}

func (r *crawlRuntime) runRegistry() *crawlruns.Registry {
	return r.runs
}

func (r *crawlRuntime) controlRegistry() *crawlbroker.ControlRegistry {
	return r.broker.Control
}

func (r *crawlRuntime) crawlQueueDepth(ctx context.Context) (crawlbroker.QueueDepth, error) {
	depth, err := r.broker.Orders.Depth(ctx)
	if err != nil {
		return crawlbroker.QueueDepth{}, fmt.Errorf("crawl runtime queue depth: %w", err)
	}

	return depth, nil
}

func (r *crawlRuntime) dispatcher() *crawldispatch.Dispatcher {
	return crawldispatch.NewDispatcher(
		r.initiator,
		mintProvenance,
		r.dispatchQueue(),
		crawldispatch.WithMaxPagesPerRun(r.MaxPagesPerRun),
	)
}

func (r *crawlRuntime) MaxPagesPerRun() int {
	if r == nil {
		return yagocrawlcontract.DefaultMaxPagesPerRun
	}

	return r.pageBudget.MaxPagesPerRun()
}

func (r *crawlRuntime) SetMaxPagesPerRun(value int) {
	if r != nil {
		r.pageBudget.Set(value)
	}
}

// recrawlSweeper builds the sweeper that drains the recrawl schedule, publishing
// due URLs straight onto the durable order queue (keyless, so a recrawl is not
// swallowed as a duplicate of its original crawl).
func (r *crawlRuntime) recrawlSweeper() recrawlSweeper {
	return recrawlSweeper{
		frontier:  r.frontier,
		publisher: keylessCrawlOrderPublisher{queue: r.dispatchQueue()},
		initiator: r.initiator,
		mint:      mintProvenance,
		now:       time.Now,
		batch:     defaultRecrawlSweepBatch,
	}
}

// crawlRecrawlSweeper returns the recrawl sweeper when the runtime is a live crawl
// runtime, or false when crawling is disabled (or the runtime is a test double),
// so the schedule is drained only when there is a fleet to re-dispatch to.
func crawlRecrawlSweeper(runtime crawlProcess) (recrawlSweeper, bool) {
	provider, ok := runtime.(interface {
		recrawlSweeper() recrawlSweeper
	})
	if !ok {
		return recrawlSweeper{}, false
	}

	return provider.recrawlSweeper(), true
}

// crawlDispatcher returns a crawl dispatcher when the crawl runtime is active, or
// nil when crawling is disabled (or the runtime is a test double), so the admin
// console's Crawler section is wired only when there is a fleet to dispatch to.
func crawlDispatcher(runtime crawlProcess) *crawldispatch.Dispatcher {
	provider, ok := runtime.(interface {
		dispatcher() *crawldispatch.Dispatcher
	})
	if !ok {
		return nil
	}

	return provider.dispatcher()
}

// crawlQueueProbe returns the crawl queue depth accessor when the runtime is a
// live crawl runtime, or nil when crawling is disabled (or the runtime is a test
// double), so the crawl queue backlog is metered only when there is a broker to
// count.
func crawlQueueProbe(runtime crawlProcess) func(context.Context) (crawlbroker.QueueDepth, error) {
	probe, ok := runtime.(interface {
		crawlQueueDepth(context.Context) (crawlbroker.QueueDepth, error)
	})
	if !ok {
		return nil
	}

	return probe.crawlQueueDepth
}

// attachCrawlMetrics wires the crawl ingest observer when the runtime supports it
// and a collector is configured, so crawl throughput is metered only for a live
// crawl runtime and never for a disabled one or a test double.
func attachCrawlMetrics(runtime crawlProcess, collector *metrics.CrawlMetrics) {
	if collector == nil {
		return
	}
	observed, ok := runtime.(interface {
		observe(crawlresults.IngestObserver)
	})
	if !ok {
		return
	}

	observed.observe(collector)
}

// crawlOrderQueue returns the crawl order queue when the crawl runtime is active,
// or nil when crawling is disabled (or the runtime is a test double), so
// web-search crawl seeding is wired only when there is a queue to publish to.
func crawlOrderQueue(runtime crawlProcess) crawldispatch.CrawlOrderQueue {
	queue, ok := runtime.(interface {
		orderQueue() crawldispatch.CrawlOrderQueue
	})
	if !ok {
		return nil
	}

	return queue.orderQueue()
}

func (r *crawlRuntime) Run(ctx context.Context) {
	r.consumer.Run(ctx)
}

func (r *crawlRuntime) Close() {
	r.broker.Close()
	if err := closeOwnedCrawlRuntimeState(r.state, r.ownsState); err != nil {
		slog.ErrorContext(
			context.Background(),
			msgCrawlRuntimeStateCloseFailed,
			slog.Any("error", err),
		)
	}
}

func mintProvenance() []byte {
	token := make([]byte, yagomodel.HashLength)
	_, _ = rand.Read(token)
	return token
}
