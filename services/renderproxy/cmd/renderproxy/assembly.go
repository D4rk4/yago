package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/cdprender"
	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/proxyintake"
	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/rendergate"
	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/rendermetrics"
	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/opsmetrics"
	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/servergroup"
)

const (
	opsReadHeaderLimit = 10 * time.Second
	shutdownLimit      = 15 * time.Second
	msgServiceStarted  = "renderproxy started"
	msgServiceStopped  = "renderproxy stopped"
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
		Handler:           opsmetrics.NewMux(metrics.Handler()),
		ReadHeaderTimeout: opsReadHeaderLimit,
	}

	slog.InfoContext(ctx, msgServiceStarted,
		slog.String("listenAddr", cfg.ListenAddr),
		slog.Int("renderConcurrency", cfg.RenderConcurrency),
	)

	err := servergroup.Run(ctx, shutdownLimit, []servergroup.NamedServer{
		{Name: "proxy", Server: proxyServer},
		{Name: "ops", Server: opsServer},
	})
	slog.InfoContext(ctx, msgServiceStopped)
	return err
}
