package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

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
		Handler:           newOpsMux(metrics.Handler()),
		ReadHeaderTimeout: opsReadHeaderLimit,
	}

	slog.InfoContext(ctx, msgServiceStarted,
		slog.String("orders", cfg.OrdersSubject),
		slog.String("listenAddr", cfg.ListenAddr),
		slog.String("opsAddr", cfg.OpsAddr),
	)

	err = runServers(ctx, publicServer, opsServer)
	slog.InfoContext(ctx, msgServiceStopped)
	return err
}

func runServers(ctx context.Context, publicServer, opsServer *http.Server) error {
	errs := make(chan error, 2)
	go func() { errs <- serve(publicServer) }()
	go func() { errs <- serve(opsServer) }()

	var serveErr error
	remaining := 2
	select {
	case <-ctx.Done():
	case serveErr = <-errs:
		remaining--
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), opsShutdownLimit)
	defer cancel()
	_ = publicServer.Shutdown(shutdownCtx)
	_ = opsServer.Shutdown(shutdownCtx)

	for ; remaining > 0; remaining-- {
		<-errs
	}
	return serveErr
}

func serve(server *http.Server) error {
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve %s: %w", server.Addr, err)
	}
	return nil
}
