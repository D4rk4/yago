package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/applog"
	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/opsmetrics"
	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/servergroup"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/eviction"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/metrics"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

const (
	version = "1.83"

	receiveBatchCap       = 1000
	receiveBusyPauseSecs  = 30
	searchPostingsPerWord = 1000
	reservoirCapacity     = 4096
	activeSetCapacity     = 256

	evictionTargetFraction = 0.9
	evictionBatch          = 256
	evictionInterval       = time.Minute

	serverReadHeaderTimeout = 10 * time.Second
	shutdownTimeout         = 15 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.ErrorContext(context.Background(), "node terminated", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	if err := applog.Configure(os.Getenv); err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}

	config, err := loadNodeConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("load node config: %w", err)
	}

	config.Crawl, err = loadCrawlConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("load crawl config: %w", err)
	}

	client := newEgressProxyClient(config.ProxyURL, outboundRequestTimeout)

	vault, err := boltvault.Open(config.StoragePath, config.StorageQuotaByte)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer closeVault(vault)

	endpoints := metrics.NewHTTPEndpointMetrics()
	metrics.NewStorageMetrics(endpoints.Registry(), vault)
	evictionMetrics := metrics.NewEvictionMetrics(endpoints.Registry())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	assembled, err := assembleNode(ctx, config, vault, client)
	if err != nil {
		return fmt.Errorf("assemble node: %w", err)
	}

	opsMux := opsmetrics.NewMux(endpoints.Handler())
	if assembled.crawl != nil {
		assembled.crawl.mountDispatch(opsMux)
	}

	return serve(
		ctx,
		assembled,
		evictionMetrics,
		servergroup.NamedServer{
			Name: "peer protocol",
			Server: buildServer(
				config.PeerAddr,
				logHTTPRequests(instrumentHTTP(endpoints, assembled.peerMux)),
			),
		},
		servergroup.NamedServer{Name: "ops", Server: buildServer(config.OpsAddr, opsMux)},
	)
}

func buildServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
	}
}

func serve(
	ctx context.Context,
	assembled node,
	evictionMetrics *metrics.EvictionMetrics,
	servers ...servergroup.NamedServer,
) error {
	workers := []func(context.Context) error{
		func(runCtx context.Context) error {
			assembled.announcer.Run(runCtx)
			return nil
		},
		func(runCtx context.Context) error {
			eviction.RunSweepLoop(runCtx, assembled.sweeper, evictionMetrics, evictionInterval)
			return nil
		},
	}
	if assembled.crawl != nil {
		defer assembled.crawl.Close()
		workers = append(workers, func(runCtx context.Context) error {
			assembled.crawl.Run(runCtx)
			return nil
		})
	}

	for _, s := range servers {
		slog.InfoContext(ctx, "serving",
			slog.String("service", s.Name),
			slog.String("addr", s.Server.Addr),
		)
	}

	return servergroup.Run(ctx, shutdownTimeout, servers, workers...)
}

func closeVault(vault *vault.Vault) {
	if err := vault.Close(); err != nil {
		slog.ErrorContext(context.Background(), "storage close failed", slog.Any("error", err))
	}
}
