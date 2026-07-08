package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/cdprender"
	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/proxyintake"
	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/rendergate"
	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/rendermetrics"
)

const (
	opsReadHeaderLimit = 10 * time.Second
	shutdownLimit      = 15 * time.Second
	msgServiceStarted  = "renderproxy started"
	msgServiceStopped  = "renderproxy stopped"
	msgShutdownFailed  = "graceful shutdown did not complete"
)

func RunService(
	ctx context.Context,
	cfg ServiceConfig,
	metrics *rendermetrics.RenderMetrics,
) error {
	browser := cdprender.New(ctx, cfg.CDPURL)
	defer browser.Close()

	gated := rendergate.New(
		browser,
		cfg.RenderConcurrency,
		cfg.RequestDeadline,
		cfg.MaxResponseBytes,
		metrics,
	)

	proxyServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           proxyintake.New(gated),
		ReadHeaderTimeout: opsReadHeaderLimit,
	}
	opsServer := &http.Server{
		Addr:              cfg.OpsAddr,
		Handler:           newOpsMux(metrics.Handler()),
		ReadHeaderTimeout: opsReadHeaderLimit,
	}

	slog.InfoContext(ctx, msgServiceStarted,
		slog.String("listenAddr", cfg.ListenAddr),
		slog.Int("renderConcurrency", cfg.RenderConcurrency),
	)

	err := runServers(ctx, proxyServer, opsServer)
	slog.InfoContext(ctx, msgServiceStopped)
	return err
}

func runServers(ctx context.Context, proxyServer, opsServer *http.Server) error {
	serverErr := make(chan error, 2)
	go func() { serverErr <- serveUntilClosed(proxyServer) }()
	go func() { serverErr <- serveUntilClosed(opsServer) }()

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-serverErr:
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownLimit)
	defer shutdownCancel()
	if err := proxyServer.Shutdown(shutdownCtx); err != nil {
		slog.WarnContext(ctx, msgShutdownFailed, slog.String("server", proxyServer.Addr), slog.Any("error", err))
	}
	if err := opsServer.Shutdown(shutdownCtx); err != nil {
		slog.WarnContext(ctx, msgShutdownFailed, slog.String("server", opsServer.Addr), slog.Any("error", err))
	}

	return runErr
}

func serveUntilClosed(server *http.Server) error {
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve %s: %w", server.Addr, err)
	}
	return nil
}
