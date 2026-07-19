// Package crawlbroker is the node's gRPC edge to the crawl fleet. It is the only
// place that speaks the CrawlExchange service: it serves a durable queue of
// crawl orders to crawler streams and receives ingest batches back, exposing
// them as the plain ports the inner packages consume. Open starts the server;
// Close stops it.
package crawlbroker

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const msgSweepFailed = "crawl lease sweep failed"

type Config struct {
	ListenAddr                        string
	LeaseTTL                          time.Duration
	FetchWorkers                      int
	ProcessPagesPerSecond             int
	MaximumRedirects                  int
	MaximumActiveRuns                 int
	DisableAutomaticDiscoveryPriority bool
	StoragePressurePolicy             yagocrawlcontract.StoragePressurePolicy
	RuntimePolicy                     yagocrawlcontract.CrawlerRuntimePolicy
	GrowthAdmission                   GrowthAdmission
}

type CrawlBroker struct {
	Orders      *DurableOrderQueue
	Ingest      *IngestReceiver
	Control     *ControlRegistry
	urlDenylist *crawlURLDenylistDelivery
	server      *grpc.Server
	listener    net.Listener
	sweep       *time.Ticker
	cancel      context.CancelFunc
}

var (
	listenCrawlRPC = func(addr string) (net.Listener, error) {
		return net.Listen("tcp", addr)
	}
	// nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- internal node-crawler control plane on a trusted network; transport security is deferred (ADR-0014).
	newGRPCServer = func() *grpc.Server {
		return grpc.NewServer(grpc.MaxRecvMsgSize(
			yagocrawlcontract.MaximumIngestMessageBytes,
		))
	}
	prepareCrawlExchange = newExchangeServerChecked
)

func Open(cfg Config, storage *vault.Vault, progress ProgressSink) (*CrawlBroker, error) {
	leaseTTL := effectiveCrawlLeaseTTL(cfg.LeaseTTL)
	queue, err := newDurableOrderQueue(storage, leaseTTL, cfg.GrowthAdmission)
	if err != nil {
		return nil, err
	}
	if cfg.DisableAutomaticDiscoveryPriority {
		queue.SetAutomaticDiscoveryPriority(false)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := queue.reconcilePriorityIndexes(ctx); err != nil {
		cancel()

		return nil, err
	}
	if err := queue.sweepExpired(ctx); err != nil {
		cancel()

		return nil, fmt.Errorf("reclaim expired crawl leases: %w", err)
	}
	controlDefaults, err := crawlerControlDefaultsFor(cfg)
	if err != nil {
		cancel()

		return nil, err
	}
	control, err := newPersistentControlRegistry(storage, controlDefaults)
	if err != nil {
		cancel()

		return nil, fmt.Errorf("open crawl control registry: %w", err)
	}
	if err := queue.replayRunControlCompletions(ctx, control); err != nil {
		cancel()

		return nil, fmt.Errorf("replay crawl run control completions: %w", err)
	}
	listener, err := listenCrawlRPC(cfg.ListenAddr)
	if err != nil {
		cancel()

		return nil, fmt.Errorf("listen crawl rpc %q: %w", cfg.ListenAddr, err)
	}

	ingest := newIngestReceiver()
	server := newGRPCServer()
	exchange, err := prepareCrawlExchange(queue, ingest.out, controlDefaults)
	if err != nil {
		_ = listener.Close()
		cancel()

		return nil, fmt.Errorf("configure crawl fetch-start schedule: %w", err)
	}
	if err := exchange.bindControl(control); err != nil {
		_ = listener.Close()
		cancel()

		return nil, fmt.Errorf("bind crawl fetch-start authority: %w", err)
	}
	exchange.urlDenylist.SetSource(nil)
	exchange.beginIngest = ingest.beginIngest
	bindCrawlProgressSink(exchange, progress)
	crawlrpc.RegisterCrawlExchangeServer(server, exchange)
	go func() { _ = server.Serve(listener) }()

	sweep := time.NewTicker(leaseSweepInterval(leaseTTL))
	go sweepLeases(ctx, queue, sweep.C)

	return &CrawlBroker{
		Orders:      queue,
		Ingest:      ingest,
		Control:     exchange.control,
		urlDenylist: exchange.urlDenylist,
		server:      server,
		listener:    listener,
		sweep:       sweep,
		cancel:      cancel,
	}, nil
}

func effectiveCrawlLeaseTTL(configured time.Duration) time.Duration {
	if configured <= 0 {
		return DefaultLeaseTTL
	}

	return configured
}

func bindCrawlProgressSink(exchange *exchangeServer, progress ProgressSink) {
	if progress != nil {
		exchange.progress = progress
	}
}

func (b *CrawlBroker) SetURLDenylistSource(source CrawlURLDenylistSource) {
	b.urlDenylist.SetSource(source)
}

func (b *CrawlBroker) Close() {
	if b == nil {
		return
	}
	if b.cancel != nil {
		b.cancel()
	}
	if b.sweep != nil {
		b.sweep.Stop()
	}
	if b.server != nil {
		b.server.Stop()
	}
}

func sweepLeases(ctx context.Context, queue *DurableOrderQueue, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			if err := queue.sweepExpired(context.WithoutCancel(ctx)); err != nil {
				slog.WarnContext(ctx, msgSweepFailed, slog.Any("error", err))
			}
		}
	}
}
