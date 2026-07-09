package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/opsmetrics"
	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/servergroup"
	"github.com/nikitakarpei/yacy-rwi-node/yacyvisitcrawl/internal/crawlorderbroker"
	"github.com/nikitakarpei/yacy-rwi-node/yacyvisitcrawl/internal/visitintake"
	"github.com/nikitakarpei/yacy-rwi-node/yacyvisitcrawl/internal/visitmetrics"
)

const (
	opsReadHeaderLimit = 10 * time.Second
	opsShutdownLimit   = 15 * time.Second
	msgServiceStarted  = "yacyvisitcrawl started"
	msgServiceStopped  = "yacyvisitcrawl stopped"
)

func RunService(ctx context.Context, cfg ServiceConfig, metrics *visitmetrics.VisitMetrics) error {
	broker, err := crawlorderbroker.Open(ctx, crawlorderbroker.Config{
		NATSURL:       cfg.NATSURL,
		OrdersSubject: cfg.OrdersSubject,
	})
	if err != nil {
		return fmt.Errorf("open crawl order broker: %w", err)
	}
	defer broker.Close()

	placement := visitintake.NewBoundedPlacement(
		broker.Orders.Place, metrics, cfg.OrderTimeout, cfg.MaxInFlight,
	)

	mux := http.NewServeMux()
	visitintake.MountVisitIntake(mux, placement, cfg.CrawlProfile, metrics, cfg.MaxBodyBytes)

	publicServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: opsReadHeaderLimit,
	}
	opsServer := &http.Server{
		Addr:              cfg.OpsAddr,
		Handler:           opsmetrics.NewMux(metrics.Handler()),
		ReadHeaderTimeout: opsReadHeaderLimit,
	}

	slog.InfoContext(ctx, msgServiceStarted,
		slog.String("orders", cfg.OrdersSubject),
		slog.String("listenAddr", cfg.ListenAddr),
		slog.String("opsAddr", cfg.OpsAddr),
	)

	err = servergroup.Run(ctx, opsShutdownLimit, []servergroup.NamedServer{
		{Name: "intake", Server: publicServer},
		{Name: "ops", Server: opsServer},
	})
	slog.InfoContext(ctx, msgServiceStopped)
	return err
}
