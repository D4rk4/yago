package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/archivemember"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/contentextraction"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlmetrics"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlrun"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawltraversal"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/htmlpage"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/httpfetch"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/orderintake"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagepublication"
)

const (
	fetchRetryLimit       = 3
	fetchRetryFloor       = 500 * time.Millisecond
	fetchRetryCeil        = 30 * time.Second
	publishRetryFloor     = 500 * time.Millisecond
	publishRetryCeil      = 30 * time.Second
	maxDeferPerURL        = 3
	containerMaxDepth     = 4
	containerMaxDocuments = 1024
	archiveMaxMembers     = 1024
	opsReadHeaderLimit    = 10 * time.Second
	opsShutdownLimit      = 15 * time.Second
	ordersAckWait         = 30 * time.Second
	msgServiceStarted     = "crawler started"
	msgServiceStopped     = "crawler stopped"
	msgCrawlStopped       = "crawl engine stopped"
)

func RunService(ctx context.Context, cfg ServiceConfig, metrics *crawlmetrics.CrawlMetrics) error {
	nc, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("init jetstream: %w", err)
	}
	if err := ensureStreams(ctx, js, cfg); err != nil {
		return err
	}

	consumer, err := ordersConsumer(ctx, js, cfg)
	if err != nil {
		return err
	}
	receiver, err := orderintake.NewOrderReceiver(ctx, consumer)
	if err != nil {
		return fmt.Errorf("start order receiver: %w", err)
	}

	fetch := httpfetch.New(
		cfg.ProxyURL,
		cfg.ProxyDialMode,
		cfg.UserAgent,
		cfg.MaxBodyBytes,
		cfg.FetchDeadline,
	)
	outputs := enabledOutputs(js, cfg)

	extract, err := buildExtractor(cfg)
	if err != nil {
		return err
	}

	crawler := crawltraversal.NewCrawler(
		traversalConfig(cfg),
		fetch,
		extract,
		crawltraversal.AlwaysDue{},
		outputs,
		metrics,
		crawltraversal.SystemClock{},
	)
	engine := crawlrun.NewEngine(metrics, crawler)

	opsServer := &http.Server{
		Addr:              cfg.OpsAddr,
		Handler:           newOpsMux(metrics.Handler()),
		ReadHeaderTimeout: opsReadHeaderLimit,
	}

	slog.InfoContext(ctx, msgServiceStarted,
		slog.String("orders", cfg.OrdersSubject),
		slog.Int("fetchConcurrency", cfg.FetchConcurrency),
		slog.Bool("indexOutput", cfg.IndexOutputEnabled),
		slog.Bool("pageOutput", cfg.PageOutputEnabled),
	)

	err = runEngineAndOps(ctx, engine, receiver, opsServer)
	slog.InfoContext(ctx, msgServiceStopped)
	return err
}

func runEngineAndOps(
	ctx context.Context,
	engine *crawlrun.Engine,
	receiver *orderintake.OrderReceiver,
	opsServer *http.Server,
) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	engineDone := make(chan struct{})
	go func() {
		if err := engine.Run(runCtx, receiver.Deliveries()); err != nil && runCtx.Err() == nil {
			slog.ErrorContext(runCtx, msgCrawlStopped, slog.Any("error", err))
		}
		close(engineDone)
	}()

	opsErr := make(chan error, 1)
	go func() {
		if err := opsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			opsErr <- err
			return
		}
		opsErr <- nil
	}()

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-opsErr:
	}
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), opsShutdownLimit)
	defer shutdownCancel()
	_ = opsServer.Shutdown(shutdownCtx)
	<-engineDone
	return serveErr
}

func ensureStreams(ctx context.Context, js jetstream.JetStream, cfg ServiceConfig) error {
	if err := yacycrawlcontract.EnsureOrdersStream(ctx, js, cfg.OrdersStreamSpec()); err != nil {
		return fmt.Errorf("ensure orders stream: %w", err)
	}
	if cfg.IndexOutputEnabled {
		if err := yacycrawlcontract.EnsureCrawledPageIndexStream(
			ctx, js, cfg.PageIndexStreamSpec(),
		); err != nil {
			return fmt.Errorf("ensure page index stream: %w", err)
		}
	}
	if cfg.PageOutputEnabled {
		if err := yacycrawlcontract.EnsureCrawledPageStream(
			ctx, js, cfg.PagesStreamSpec(),
		); err != nil {
			return fmt.Errorf("ensure pages stream: %w", err)
		}
	}
	return nil
}

func ordersConsumer(
	ctx context.Context,
	js jetstream.JetStream,
	cfg ServiceConfig,
) (jetstream.Consumer, error) {
	consumer, err := js.CreateOrUpdateConsumer(ctx, yacycrawlcontract.OrdersStreamName,
		jetstream.ConsumerConfig{
			Durable:       cfg.OrdersDurable,
			FilterSubject: cfg.OrdersSubject,
			AckPolicy:     jetstream.AckExplicitPolicy,
			AckWait:       ordersAckWait,
			MaxAckPending: 1,
		})
	if err != nil {
		return nil, fmt.Errorf("create orders consumer: %w", err)
	}
	return consumer, nil
}

func enabledOutputs(js jetstream.JetStream, cfg ServiceConfig) []crawlcapability.PagePublication {
	var outputs []crawlcapability.PagePublication
	if cfg.IndexOutputEnabled {
		outputs = append(outputs, pagepublication.NewIndexOutput(js, cfg.PageIndexSubject))
	}
	if cfg.PageOutputEnabled {
		outputs = append(outputs, pagepublication.NewPageContentOutput(js, cfg.PagesSubject))
	}
	return outputs
}

func buildExtractor(cfg ServiceConfig) (crawlcapability.DocumentExtraction, error) {
	allow := allowedMediaTypes(cfg.ContentTypes)
	router := contentextraction.New(containerMaxDepth, containerMaxDocuments)

	html := htmlpage.New()
	for _, mediaType := range html.MediaTypes() {
		if allow == nil || allow[mediaType] {
			router.RegisterExtractor(mediaType, html)
		}
	}

	archive := archivemember.New(archiveMaxMembers, cfg.MaxBodyBytes)
	for _, mediaType := range archive.MediaTypes() {
		if allow == nil || allow[mediaType] {
			router.RegisterContainer(mediaType, archive)
		}
	}

	if router.RegisteredMediaTypes() == 0 {
		return nil, fmt.Errorf("%s: leaves no content extractor active", EnvContentTypes)
	}
	return router, nil
}

func allowedMediaTypes(contentTypes []string) map[string]bool {
	if len(contentTypes) == 0 {
		return nil
	}
	allow := make(map[string]bool, len(contentTypes))
	for _, mediaType := range contentTypes {
		allow[mediaType] = true
	}
	return allow
}

func traversalConfig(cfg ServiceConfig) crawltraversal.Config {
	return crawltraversal.Config{
		RunPageBudget:       cfg.RunPageBudget,
		FrontierCapacity:    cfg.FrontierCap,
		FetchRetryLimit:     fetchRetryLimit,
		FetchRetryFloor:     fetchRetryFloor,
		FetchRetryCeiling:   fetchRetryCeil,
		PublishRetryFloor:   publishRetryFloor,
		PublishRetryCeiling: publishRetryCeil,
		MaxDeferralsPerURL:  maxDeferPerURL,
		FetchConcurrency:    cfg.FetchConcurrency,
		OwnershipInterval:   ordersAckWait / 2,
	}
}
