package yagonode

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
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

	broker, err := openCrawlBroker(crawlbroker.Config{ListenAddr: config.ListenAddr}, storageVault)
	if err != nil {
		return nil, fmt.Errorf("open crawl broker: %w", err)
	}

	consumer := crawlresults.NewIngestConsumerWithIndex(
		broker.Ingest,
		storage.documentReceiver,
		storage.searchIndex,
		storage.urlReceiver,
		storage.postingReceiver,
	)

	return &crawlRuntime{
		broker:    broker,
		consumer:  consumer,
		initiator: identity.Hash,
	}, nil
}

func (r *crawlRuntime) mountDispatch(mux *http.ServeMux) {
	crawldispatch.MountCrawlDispatch(mux, r.initiator, mintProvenance, r.broker.Orders)
}

func (r *crawlRuntime) observe(observer crawlresults.IngestObserver) {
	r.consumer.Observe(observer)
}

func (r *crawlRuntime) orderQueue() crawldispatch.CrawlOrderQueue {
	return r.broker.Orders
}

func (r *crawlRuntime) dispatcher() *crawldispatch.Dispatcher {
	return crawldispatch.NewDispatcher(r.initiator, mintProvenance, r.broker.Orders)
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
