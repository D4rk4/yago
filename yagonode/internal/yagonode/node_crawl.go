package yagonode

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
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
	broker    *crawlbroker.CrawlBroker
	consumer  *crawlresults.IngestConsumer
	runs      *crawlruns.Registry
	frontier  *recrawlfrontier.Frontier
	initiator yagomodel.Hash
}

var openCrawlBroker = crawlbroker.Open

func buildCrawlRuntime(
	config crawlConfig,
	identity nodeidentity.Identity,
	storage nodeStorage,
	storageVault *vault.Vault,
) (*crawlRuntime, error) {
	if !config.Enabled() {
		return nil, nil
	}

	runs := crawlruns.New(0)
	broker, err := openCrawlBroker(
		crawlbroker.Config{ListenAddr: config.ListenAddr},
		storageVault,
		runs,
	)
	if err != nil {
		return nil, fmt.Errorf("open crawl broker: %w", err)
	}

	frontier, err := recrawlfrontier.Open(storageVault)
	if err != nil {
		return nil, fmt.Errorf("open recrawl frontier: %w", err)
	}

	consumer := crawlresults.NewIngestConsumerWithIndex(
		broker.Ingest,
		storage.documentReceiver,
		storage.searchIndex,
		storage.urlReceiver,
		storage.postingReceiver,
	)
	consumer.RecordFetches(frontier)

	return &crawlRuntime{
		broker:    broker,
		consumer:  consumer,
		runs:      runs,
		frontier:  frontier,
		initiator: identity.Hash,
	}, nil
}

// dispatchQueue wraps the durable order queue so every dispatched or seeded crawl
// order records its profile in the recrawl frontier before it is enqueued.
func (r *crawlRuntime) dispatchQueue() crawldispatch.CrawlOrderQueue {
	return profileRecordingQueue{inner: r.broker.Orders, frontier: r.frontier}
}

func (r *crawlRuntime) mountDispatch(mux *http.ServeMux) {
	crawldispatch.MountCrawlDispatch(mux, r.initiator, mintProvenance, r.dispatchQueue())
}

func (r *crawlRuntime) observe(observer crawlresults.IngestObserver) {
	r.consumer.Observe(observer)
}

func (r *crawlRuntime) orderQueue() crawldispatch.CrawlOrderQueue {
	return r.dispatchQueue()
}

func (r *crawlRuntime) runRegistry() *crawlruns.Registry {
	return r.runs
}

func (r *crawlRuntime) crawlQueueDepth(ctx context.Context) (crawlbroker.QueueDepth, error) {
	depth, err := r.broker.Orders.Depth(ctx)
	if err != nil {
		return crawlbroker.QueueDepth{}, fmt.Errorf("crawl runtime queue depth: %w", err)
	}

	return depth, nil
}

func (r *crawlRuntime) dispatcher() *crawldispatch.Dispatcher {
	return crawldispatch.NewDispatcher(r.initiator, mintProvenance, r.dispatchQueue())
}

// recrawlSweeper builds the sweeper that drains the recrawl schedule, publishing
// due URLs straight onto the durable order queue (keyless, so a recrawl is not
// swallowed as a duplicate of its original crawl).
func (r *crawlRuntime) recrawlSweeper() recrawlSweeper {
	return recrawlSweeper{
		frontier:  r.frontier,
		publisher: r.broker.Orders,
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
}

func mintProvenance() []byte {
	token := make([]byte, yagomodel.HashLength)
	_, _ = rand.Read(token)
	return token
}
