package yagonode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
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

	serverReadHeaderTimeout = 10 * time.Second
	shutdownTimeout         = 15 * time.Second
)

var (
	exitProcess         = os.Exit
	runNode             = run
	openRuntimeVault    = boltvault.Open
	assembleRuntimeNode = assembleNode
	serveRuntimeNode    = serve
	listenAndServeHTTP  = func(server *http.Server) error { return server.ListenAndServe() }
	shutdownHTTPServer  = func(server *http.Server, ctx context.Context) error { return server.Shutdown(ctx) }
)

func Main() {
	if err := runNode(); err != nil {
		slog.ErrorContext(context.Background(), "node terminated", slog.Any("error", err))
		exitProcess(1)
	}
}

func run() error {
	if err := configureLogging(os.Getenv); err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}

	config, err := loadNodeConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("load node config: %w", err)
	}

	config.Crawl = loadCrawlConfig(os.Getenv)
	config.Admin = loadAdminConfig(os.Getenv)
	config.CrossOrigin = loadCrossOriginConfig(os.Getenv)

	client := newRuntimeEgressClient(config)

	vault, err := openRuntimeVault(config.StoragePath, config.StorageQuotaByte)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer closeVault(vault)

	endpoints := metrics.NewHTTPEndpointMetrics()
	metrics.NewStorageMetrics(endpoints.Registry(), vault)
	evictionMetrics := metrics.NewEvictionMetrics(endpoints.Registry())
	dhtOutboundMetrics := metrics.NewDHTOutboundMetrics(endpoints.Registry())
	dhtInboundMetrics := metrics.NewDHTInboundMetrics(endpoints.Registry())
	peerMetrics := metrics.NewPeerMetrics(endpoints.Registry())
	authMetrics := metrics.NewAuthMetrics(endpoints.Registry())
	eventRecorder := events.NewRecorder(events.DefaultCapacity)
	authObserver := authObserverFanOut{authMetrics, authEventObserver{recorder: eventRecorder}}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	authService, err := provisionAdminAuth(ctx, config, vault, authObserver)
	if err != nil {
		return fmt.Errorf("configure admin auth: %w", err)
	}

	assembled, err := assembleRuntimeNode(
		ctx,
		config,
		vault,
		client,
		nodeTelemetry{
			dhtOutbound:      dhtOutboundMetrics,
			dhtInbound:       dhtInboundMetrics,
			peer:             peerMetrics,
			searchAuthorizer: searchScopeAuthorizerFor(config, authService),
		},
	)
	if err != nil {
		return fmt.Errorf("assemble node: %w", err)
	}

	opsMux := buildOpsMux(endpoints, config, assembled, eventRecorder)
	opsHandler := wrapAdminCORS(
		config.CrossOrigin.AdminOrigins,
		guardAdminSurface(authService, opsMux),
	)

	return serveRuntimeNode(
		ctx,
		assembled,
		evictionMetrics,
		namedServer{
			"peer protocol",
			buildServer(
				config.PeerAddr,
				wrapSearchCORS(
					config.CrossOrigin.SearchOrigins,
					logHTTPRequests(instrumentHTTP(endpoints, assembled.peerMux)),
				),
			),
		},
		namedServer{"ops", buildServer(config.OpsAddr, opsHandler)},
	)
}

type namedServer struct {
	name   string
	server *http.Server
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
	servers ...namedServer,
) error {
	ctx, cancel := context.WithCancel(ctx)

	var background sync.WaitGroup
	background.Add(2)
	go func() {
		defer background.Done()
		assembled.announcer.Run(ctx)
	}()
	go func() {
		defer background.Done()
		runEvictionLoop(ctx, assembled.sweeper, evictionMetrics)
	}()
	if assembled.crawl != nil {
		defer assembled.crawl.Close()
		background.Add(1)
		go func() {
			defer background.Done()
			assembled.crawl.Run(ctx)
		}()
	}
	if assembled.dht.cycle != nil {
		background.Add(1)
		go func() {
			defer background.Done()
			runDHTOutboundLoop(ctx, assembled.dht)
		}()
	}
	defer background.Wait()
	defer cancel()

	errs := make(chan error, len(servers))
	for _, s := range servers {
		go func(s namedServer) {
			slog.InfoContext(
				ctx,
				"serving",
				slog.String("service", s.name),
				slog.String("addr", s.server.Addr),
			)
			errs <- listenAndServeHTTP(s.server)
		}(s)
	}

	select {
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return shutdown(servers)
		}

		return err
	case <-ctx.Done():
		return shutdown(servers)
	}
}

func shutdown(servers []namedServer) error {
	slog.InfoContext(context.Background(), "shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	var failures error
	for _, s := range servers {
		if err := shutdownHTTPServer(s.server, ctx); err != nil {
			failures = errors.Join(failures, fmt.Errorf("shutdown %s: %w", s.name, err))
		}
	}

	return failures
}

type vaultCloser interface {
	Close() error
}

func closeVault(storage vaultCloser) {
	if err := storage.Close(); err != nil {
		slog.ErrorContext(context.Background(), "storage close failed", slog.Any("error", err))
	}
}
