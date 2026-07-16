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

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/metrichistory"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/shardvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	// version is the numeric YaCy-compatible protocol version advertised in the
	// seed's Version field. YaCy peers parse it with Float/Double.parseFloat and
	// gate features on it, so it must stay a plain float string; it tracks the
	// current YaCy release (build.properties releaseVersion) so peers treat this
	// node as a current participant rather than a stale one. It is a constant
	// deliberately: the protocol version must never be overridden at build time.
	version = "1.941"

	receiveBatchCap      = 1000
	receiveBusyPauseSecs = 30
	// dhtInboundTransferSlots bounds concurrent inbound transferRWI/transferURL
	// intake, and inboundRemoteSearchSlots bounds concurrent /yacy/search.html
	// serving; excess requests are shed with protocol-level busy answers.
	dhtInboundTransferSlots  = 4
	inboundRemoteSearchSlots = 8
	searchPostingsPerWord    = 1000
	reservoirCapacity        = 4096
	activeSetCapacity        = 256

	evictionTargetFraction = 0.9
	evictionBatch          = 256

	serverReadHeaderTimeout = 10 * time.Second
	serverReadTimeout       = 2 * time.Minute
	serverIdleTimeout       = 2 * time.Minute
	serverMaxHeaderBytes    = 64 << 10
	shutdownTimeout         = 15 * time.Second
	shutdownForcedWait      = 5 * time.Second
)

var buildVersion = "2026.07.16-dev"

// Version returns the product build identity stamped into this binary. It is
// what `yago-node --version` reports.
func Version() string { return buildVersion }

// userAgent brands this node's outbound requests as yago while declaring the YaCy
// protocol version it speaks. It is applied only where a caller has not already
// set its own User-Agent (see egress_client.go). It derives from buildVersion at
// startup, so a stamped build is reflected here too.
var userAgent = "yago/" + buildVersion +
	" (+https://github.com/D4rk4/yago; YaCy/" + version + " compatible)"

var (
	exitProcess      = os.Exit
	runNode          = run
	openRuntimeVault = func(path string, quotaBytes int64) (*vault.Vault, error) {
		return shardvault.OpenAt(path, quotaBytes,
			shardvault.WithWordFilter(rwi.PostingsBucket, yagomodel.HashLength))
	}
	assembleRuntimeNode = assembleNode
	serveRuntimeNode    = serve
	listenAndServeHTTP  = func(server *http.Server) error { return server.ListenAndServe() }
	shutdownHTTPServer  = func(server *http.Server, ctx context.Context) error { return server.Shutdown(ctx) }
	closeHTTPServer     = func(server *http.Server) error { return server.Close() }
)

func Main() {
	err := runNode()
	if errors.Is(err, errRestartRequested) {
		slog.InfoContext(
			context.Background(),
			"restarting to apply settings",
			slog.Int("exitCode", restartExitCode),
		)
		exitProcess(restartExitCode)

		return
	}
	if err != nil {
		slog.ErrorContext(context.Background(), "node terminated", slog.Any("error", err))
		exitProcess(1)
	}
}

func run() error {
	getenv := withLegacyEnvAliases(os.Getenv)

	if err := configureLogging(getenv); err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}

	config, err := loadNodeConfig(getenv)
	if err != nil {
		return fmt.Errorf("load node config: %w", err)
	}

	config.Crawl, err = loadCrawlConfig(getenv)
	if err != nil {
		return fmt.Errorf("load crawl config: %w", err)
	}
	config.Admin = loadAdminConfig(getenv)
	config.CrossOrigin = loadCrossOriginConfig(getenv)

	client := newRuntimeEgressClient(config)

	storageVault, err := openRuntimeVault(config.StoragePath, config.StorageQuotaByte)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	var eventDrain <-chan struct{}
	defer func() { closeVaultAfterEventDrain(storageVault, eventDrain) }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	eventDrain, err = bootNodeWithEventDrain(ctx, config, storageVault, client)

	return err
}

// bootNode drives the node once its configuration is loaded and its durable
// storage is open: it wires observability, applies runtime overrides, validates
// the listen addresses, assembles the surfaces, and serves until the context is
// cancelled. It is separated from run so the post-storage boot can be exercised
// against an injected vault.
func bootNode(
	ctx context.Context,
	config nodeConfig,
	storageVault *vault.Vault,
	client *http.Client,
) error {
	_, err := bootNodeWithEventDrain(ctx, config, storageVault, client)

	return err
}

func bootNodeWithEventDrain(
	ctx context.Context,
	config nodeConfig,
	storageVault *vault.Vault,
	client *http.Client,
) (<-chan struct{}, error) {
	obs, err := provisionObservability(ctx, storageVault)
	if err != nil {
		return nil, err
	}
	defer closeEventPersistence(obs.persistence)
	eventDrain := obs.persistence.done

	sources, toggles, config, err := loadRuntimeSettings(ctx, storageVault, config, obs.recorder)
	if err != nil {
		return eventDrain, err
	}
	storageVault.SetQuota(config.StorageQuotaByte)
	storageVault.SetDeferredFsync(config.StorageDeferFsync)
	storageVault.SetReadDeferBudget(config.StorageReadDefer)
	if err := validateNodeBinds(config); err != nil {
		return eventDrain, fmt.Errorf("validate listen addresses: %w", err)
	}

	authService, err := provisionAdminAuth(ctx, config, storageVault, obs.authObserver)
	if err != nil {
		return eventDrain, fmt.Errorf("configure admin auth: %w", err)
	}
	sources.security = newSecuritySource(authService)
	// The serve context is governed by the restart controller so the setup
	// wizard can end in a mandatory graceful restart (its choices apply at boot).
	ctx, restart := newRestartController(ctx)
	configureSetupWizard(authService, sources.settings, config, restart.Trigger)
	sources.restart = restart.Trigger
	historySampler := metrichistory.New(
		obs.endpoints.Registry(),
		performanceHistoryCapacity,
		currentHostMemory,
	)
	defer startPerformanceHistorySampler(ctx, historySampler)()
	sources.perfHistory = newPerformanceHistorySource(historySampler)

	assembled, err := assembleRuntimeNode(
		ctx,
		config,
		storageVault,
		client,
		nodeTelemetry{
			dhtOutbound:      obs.dhtOutbound,
			dhtInbound:       obs.dhtInbound,
			peer:             obs.peer,
			search:           obs.search,
			crawl:            obs.crawl,
			indexWrites:      obs.indexWrites,
			crawlRuns:        obs.crawlRuns,
			recorder:         obs.recorder,
			searchAuthorizer: searchScopeAuthorizerFor(config, authService),
			toggles:          toggles,
			saturation:       obs.saturation,
		},
	)
	if err != nil {
		return eventDrain, fmt.Errorf("assemble node: %w", err)
	}
	defer assembled.clicks.StopImpressionPreparations()

	opsMux := buildOpsMux(obs.endpoints, config, assembled, obs.recorder, sources)
	opsHandler := wrapAdminCORS(
		config.CrossOrigin.AdminOrigins,
		guardAdminSurface(authService, opsMux),
	)

	servers := []namedServer{
		buildPeerServer(config, obs.endpoints, assembled, toggles),
		{"ops", buildServer(config.OpsAddr, redirectHTTPS(toggles, opsHandler))},
	}
	if config.PublicAddr != "" {
		servers = append(servers, buildPublicServer(config, obs.endpoints, assembled, toggles))
	}

	return eventDrain, restart.Wrap(serveRuntimeNode(ctx, assembled, obs.eviction, servers...))
}

// buildPublicServer builds the dedicated public search listener: the portal,
// OpenSearch, the Tavily-compatible API, and the /yacysearch.* surfaces. It is
// only constructed when a public address is configured, so a pure peer node runs
// without it.
func buildPublicServer(
	config nodeConfig,
	endpoints *metrics.HTTPEndpointMetrics,
	assembled node,
	toggles *runtimeToggles,
) namedServer {
	publicHandler := redirectHTTPS(toggles, wrapSearchCORS(
		config.CrossOrigin.SearchOrigins,
		logHTTPRequests(instrumentHTTP(endpoints, assembled.publicMux)),
	))

	return namedServer{"public search", buildServer(config.PublicAddr, publicHandler)}
}

func buildPeerServer(
	config nodeConfig,
	endpoints *metrics.HTTPEndpointMetrics,
	assembled node,
	toggles *runtimeToggles,
) namedServer {
	peerHandler := redirectHTTPS(toggles, wrapSearchCORS(
		config.CrossOrigin.SearchOrigins,
		logHTTPRequests(instrumentHTTP(endpoints, assembled.peerMux)),
	))

	return namedServer{"peer protocol", buildServer(config.PeerAddr, peerHandler)}
}

type namedServer struct {
	name   string
	server *http.Server
}

func buildServer(addr string, handler http.Handler) *http.Server {
	requests := newHTTPRequestLifecycle(handler)

	return &http.Server{
		Addr:              addr,
		Handler:           requests,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
}

func serve(
	ctx context.Context,
	assembled node,
	evictionMetrics *metrics.EvictionMetrics,
	servers ...namedServer,
) error {
	ctx, cancel := context.WithCancel(ctx)
	listenAndServe := listenAndServeHTTP
	var background sync.WaitGroup
	background.Add(3)
	startPeerPresenceLoops(ctx, &background, assembled)
	startRedirectPurge(ctx, &background, assembled)
	startCrawlScheduleLoop(ctx, &background, assembled)
	go func() {
		defer background.Done()
		runEvictionLoop(ctx, assembled.sweeper, evictionMetrics)
	}()
	go func() {
		defer background.Done()
		runCorpusSignalRefreshLoop(ctx, corpusSignalRefreshForNode(ctx, assembled))
	}()
	startMaintenanceLoops(ctx, &background, assembled)
	if assembled.crawl != nil {
		defer assembled.crawl.Close()
		background.Add(1)
		go func() {
			defer background.Done()
			assembled.crawl.Run(ctx)
		}()
		if sweeper, ok := crawlRecrawlSweeper(assembled.crawl); ok {
			background.Add(1)
			go func() {
				defer background.Done()
				runRecrawlSweepLoop(ctx, sweeper)
			}()
		}
	}
	if assembled.dht.cycle != nil {
		background.Add(1)
		go func() {
			defer background.Done()
			runDHTOutboundLoop(ctx, assembled.dht)
		}()
	}
	defer background.Wait()
	defer stopPeerReputation(cancel, assembled.peerEvents)

	errs := make(chan error, len(servers))
	for _, s := range servers {
		go func(s namedServer) {
			slog.InfoContext(
				ctx,
				"serving",
				slog.String("service", s.name),
				slog.String("addr", s.server.Addr),
			)
			errs <- listenAndServe(s.server)
		}(s)
	}
	select {
	case err := <-errs:
		return settleListenerExit(err, servers)
	case <-ctx.Done():
		return shutdown(servers)
	}
}

// startPeerPresenceLoops runs the DHT announcer and the LAN discovery beacon;
// the beacon is nil on deployments that disabled discovery, and Run on a nil
// beacon is a no-op.
func startPeerPresenceLoops(ctx context.Context, background *sync.WaitGroup, assembled node) {
	go func() {
		defer background.Done()
		assembled.announcer.Run(ctx)
	}()
	background.Add(1)
	go func() {
		defer background.Done()
		assembled.lanBeacon.Run(ctx)
	}()
}

// startMaintenanceLoops runs the background storage-maintenance passes: periodic
// compaction (ADR-0036 C), automatic shard growth (ADR-0037), and the
// deferred-fsync flush that backstops NoSync mode (ADR-0038).
func startMaintenanceLoops(ctx context.Context, background *sync.WaitGroup, assembled node) {
	background.Add(3)
	go func() {
		defer background.Done()
		runCompactionLoop(ctx, assembled.vault, assembled.toggles)
	}()
	go func() {
		defer background.Done()
		runShardGrowthLoop(ctx, assembled.vault, assembled.toggles)
	}()
	go func() {
		defer background.Done()
		runDeferredSyncLoop(ctx, assembled.vault)
	}()
}

type vaultCloser interface {
	Close() error
}

func closeVault(storage vaultCloser) {
	if err := storage.Close(); err != nil {
		slog.ErrorContext(context.Background(), "storage close failed", slog.Any("error", err))
	}
}

func closeVaultAfterEventDrain(storage vaultCloser, done <-chan struct{}) {
	if done == nil {
		closeVault(storage)

		return
	}
	select {
	case <-done:
		closeVault(storage)
	default:
		go func() {
			<-done
			closeVault(storage)
		}()
	}
}
