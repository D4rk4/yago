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
	"github.com/D4rk4/yago/yagonode/internal/metrics"
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
	shutdownTimeout         = 15 * time.Second
)

// buildVersion is yago's own calendar build version (YYYY.M), a brand identity
// kept separate from the numeric YaCy protocol version so the two evolve
// independently. It is a var, not a const, so a release build can stamp a precise
// version through -ldflags "-X ...yagonode.buildVersion=<ver>" (see the
// Dockerfile); left unstamped it reports the calendar default.
var buildVersion = "2026.7"

// userAgent brands this node's outbound requests as yago while declaring the YaCy
// protocol version it speaks. It is applied only where a caller has not already
// set its own User-Agent (see egress_client.go). It derives from buildVersion at
// startup, so a stamped build is reflected here too.
var userAgent = "yago/" + buildVersion +
	" (+https://github.com/D4rk4/yago; YaCy/" + version + " compatible)"

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
	getenv := withLegacyEnvAliases(os.Getenv)

	if err := configureLogging(getenv); err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}

	config, err := loadNodeConfig(getenv)
	if err != nil {
		return fmt.Errorf("load node config: %w", err)
	}

	config.Crawl = loadCrawlConfig(getenv)
	config.Admin = loadAdminConfig(getenv)
	config.CrossOrigin = loadCrossOriginConfig(getenv)

	client := newRuntimeEgressClient(config)

	storageVault, err := openRuntimeVault(config.StoragePath, config.StorageQuotaByte)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer closeVault(storageVault)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return bootNode(ctx, config, storageVault, client)
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
	obs, err := provisionObservability(ctx, storageVault)
	if err != nil {
		return err
	}

	sources, toggles, config, err := loadRuntimeSettings(ctx, storageVault, config, obs.recorder)
	if err != nil {
		return err
	}
	if err := validateNodeBinds(config); err != nil {
		return fmt.Errorf("validate listen addresses: %w", err)
	}

	authService, err := provisionAdminAuth(ctx, config, storageVault, obs.authObserver)
	if err != nil {
		return fmt.Errorf("configure admin auth: %w", err)
	}
	sources.security = newSecuritySource(authService)

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
			crawlRuns:        obs.crawlRuns,
			recorder:         obs.recorder,
			searchAuthorizer: searchScopeAuthorizerFor(config, authService),
			toggles:          toggles,
		},
	)
	if err != nil {
		return fmt.Errorf("assemble node: %w", err)
	}

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

	return serveRuntimeNode(ctx, assembled, obs.eviction, servers...)
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
	background.Add(3)
	go func() {
		defer background.Done()
		assembled.announcer.Run(ctx)
	}()
	go func() {
		defer background.Done()
		runEvictionLoop(ctx, assembled.sweeper, evictionMetrics)
	}()
	go func() {
		defer background.Done()
		runHostRankRefreshLoop(ctx, hostRankSweeper{
			documents: assembled.docScan,
			holder:    assembled.hostRank,
		})
	}()
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
