package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/D4rk4/yago/yago-crawler/internal/botwall"
	"github.com/D4rk4/yago/yago-crawler/internal/crawldelay"
	"github.com/D4rk4/yago/yago-crawler/internal/crawldenylist"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlseed"
	"github.com/D4rk4/yago/yago-crawler/internal/httpfetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/publicweb"
	"github.com/D4rk4/yago/yago-crawler/internal/robots"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagoegress"
)

var newCrawlerExchange = func(addr string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(
			yagocrawlcontract.MaximumIngestMessageBytes,
		)),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("new crawl exchange client: %w", err)
	}

	return crawlrpc.NewCrawlExchangeClient(conn), conn, nil
}

var newCrawlerRobotsAdmissionFetcher = robots.NewRobotsAdmissionFetcher

var newCrawlerHTTPPageFetcher = httpfetch.NewPageFetcher

var newCrawlerSeedSource = crawlseed.NewHTTPSource

var newCrawlerPublicWebAdmissionFetcher = func(
	inner pagefetch.PageSource,
	resolver publicweb.Resolver,
	guard yagoegress.Guard,
) pagefetch.PageSource {
	return publicweb.NewAdmissionFetcher(inner, resolver, guard)
}

// fetchChains carries the two assembled page-fetch chains: the verifying
// default and the parallel one for profiles that opted into
// IgnoreTLSAuthority. Both share the browser fallback and the botwall,
// robots, and public-web layers; only the TLS client differs.
type fetchChains struct {
	verifying pagefetch.PageSource
	insecure  pagefetch.PageSource
	// verifyingDirect and insecureDirect skip the robots.txt layer for jobs
	// whose profile set IgnoreRobots; every other layer (botwall, browser
	// fallback, public-web admission) is identical.
	verifyingDirect pagefetch.PageSource
	insecureDirect  pagefetch.PageSource
}

func buildFetchChains(
	guard yagoegress.Guard,
	client *http.Client,
	crawl CrawlConfig,
	source pagefetch.PageSource,
	metrics *crawlermetrics.Metrics,
) (fetchChains, error) {
	slowSource := botwall.NewBotWallScreeningFetcher(source)
	fastSource := botwall.NewBotWallScreeningFetcher(
		newCrawlerHTTPPageFetcher(client, crawl.UserAgent, crawl.MaxBodyBytes).
			WithHTTP1Fallback(newHTTP1EgressClient(guard, crawl, nil)),
	)
	verifyingCore := pagefetch.NewBrowserFallbackPageSource(
		fastSource,
		slowSource,
		browserRenderNeeded,
	)
	admitted, err := newCrawlerRobotsAdmissionFetcher(
		verifyingCore,
		client,
		crawl.UserAgent,
		crawl.HostCacheSize,
		robots.WithDenialObserver(metrics),
	)
	if err != nil {
		return fetchChains{}, fmt.Errorf("create robots admission: %w", err)
	}

	insecureClient := newInsecureEgressClient(guard, crawl)
	insecureFast := botwall.NewBotWallScreeningFetcher(
		newCrawlerHTTPPageFetcher(insecureClient, crawl.UserAgent, crawl.MaxBodyBytes).
			WithHTTP1Fallback(newHTTP1EgressClient(guard, crawl, insecureTLSConfig())),
	)
	insecureCore := pagefetch.NewBrowserFallbackPageSource(
		insecureFast,
		slowSource,
		browserRenderNeeded,
	)
	insecureAdmitted, err := newCrawlerRobotsAdmissionFetcher(
		insecureCore,
		insecureClient,
		crawl.UserAgent,
		crawl.HostCacheSize,
		robots.WithDenialObserver(metrics),
	)
	if err != nil {
		return fetchChains{}, fmt.Errorf("create insecure robots admission: %w", err)
	}

	return fetchChains{
		verifying: pagefetch.NewDuplicatePageFetchSuppressor(
			newCrawlerPublicWebAdmissionFetcher(admitted, nil, guard),
		),
		insecure: pagefetch.NewDuplicatePageFetchSuppressor(
			newCrawlerPublicWebAdmissionFetcher(insecureAdmitted, nil, guard),
		),
		verifyingDirect: pagefetch.NewDuplicatePageFetchSuppressor(
			newCrawlerPublicWebAdmissionFetcher(verifyingCore, nil, guard),
		),
		insecureDirect: pagefetch.NewDuplicatePageFetchSuppressor(
			newCrawlerPublicWebAdmissionFetcher(insecureCore, nil, guard),
		),
	}, nil
}

var newCrawlerAdaptivePace = crawldelay.NewAdaptivePace

func RunService(ctx context.Context, cfg ServiceConfig, source pagefetch.PageSource) error {
	return runServiceWithMetrics(ctx, cfg, source, crawlermetrics.New())
}

func runServiceWithMetrics(
	ctx context.Context, cfg ServiceConfig, source pagefetch.PageSource,
	metrics *crawlermetrics.Metrics,
) error {
	ctx, restart := newRestartController(ctx)
	ctx, stopService := context.WithCancel(ctx)
	defer stopService()
	checkpointSession, err := openCrawlerCheckpointSession(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = checkpointSession.checkpoint.Close() }()
	cfg.WorkerID = checkpointSession.workerID
	storagePressure := newCrawlerStorageAdmission(cfg, metrics)

	nodeRPC, err := openCrawlerNodeRPC(cfg.NodeRPCAddr)
	if err != nil {
		return err
	}
	defer nodeRPC.close()

	emitter := newCrawlerIngestEmitter(nodeRPC, checkpointSession)

	metricsCloser, err := startCrawlerMetrics(ctx, cfg.MetricsAddr, metrics.Handler())
	if err != nil {
		return fmt.Errorf("start crawler metrics: %w", err)
	}
	defer func() { _ = metricsCloser.Close() }()

	crawl := cfg.Crawl
	redirects := newRedirectLimit(crawl.MaxRedirects)
	crawl.redirectLimit = redirects
	cfg.Crawl = crawl
	receiverCtx, cancelReceiver := context.WithCancel(ctx)
	defer cancelReceiver()
	execution, err := assembleCrawlerExecution(crawlerExecutionStart{
		context:          receiverCtx,
		config:           cfg,
		source:           source,
		metrics:          metrics,
		checkpoint:       checkpointSession,
		nodeRPC:          nodeRPC,
		emitter:          emitter,
		growthAdmission:  storagePressure,
		restart:          restart.Trigger,
		shutdown:         stopService,
		maximumRedirects: redirects,
	})
	if err != nil {
		return err
	}
	lifecycle, err := execution.lifecycle(cfg, source, metrics)
	if err != nil {
		return err
	}
	runCrawlerLifecycle(ctx, lifecycle, cfg)

	return restart.Wrap(execution.frontier.CheckpointFailure())
}

func newCrawlRequestExpander(
	client *http.Client,
	crawl CrawlConfig,
	guard yagoegress.Guard,
	urlDenylist *crawldenylist.Denylist,
) *crawlseed.Expander {
	seedSource := newCrawlerPublicWebAdmissionFetcher(
		newCrawlerSeedSource(client, crawl.UserAgent, crawl.MaxBodyBytes),
		nil,
		guard,
	)
	seedSource = crawldenylist.NewAdmissionFetcher(seedSource, urlDenylist)
	return crawlseed.NewExpander(seedSource, crawl.SitemapURLLimit)
}
