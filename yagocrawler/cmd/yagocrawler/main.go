package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/D4rk4/yago/yagocrawler/internal/chromedpfetch"
	"github.com/D4rk4/yago/yagoegress"
)

var exitProcess = os.Exit

var loadCrawlerServiceConfig = LoadServiceConfig

var notifyProcessContext = signal.NotifyContext

var runConfiguredCrawler = run

var newCrawlerBrowserFetcher = chromedpfetch.NewBrowserPageFetcher

var runCrawlerService = RunService

func main() {
	exitProcess(start())
}

func start() int {
	cfg, err := loadCrawlerServiceConfig(os.Getenv)
	if err != nil {
		slog.ErrorContext(context.Background(), "crawler config invalid", slog.Any("error", err))
		return 2
	}
	ctx, stop := notifyProcessContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch err := runConfiguredCrawler(ctx, cfg); {
	case errors.Is(err, errRestartRequested):
		slog.InfoContext(ctx, "crawler restarting", slog.Int("exitCode", restartExitCode))
		return restartExitCode
	case err != nil:
		slog.ErrorContext(ctx, "crawler failed", slog.Any("error", err))
		return 1
	default:
		return 0
	}
}

func run(ctx context.Context, cfg ServiceConfig) error {
	crawl := cfg.Crawl
	fetcher, closeBrowser, err := newCrawlerBrowserFetcher(
		chromedpfetch.BrowserLaunch{
			UserAgent: crawl.UserAgent,
			Timeout:   crawl.RequestTimeout,
			MaxBytes:  crawl.MaxBodyBytes,
			ExecPath:  crawl.BrowserPath,
			Sandbox:   crawl.BrowserSandbox,
		},
		yagoegress.NewGuard(
			cfg.EgressAllowLAN,
			yagoegress.WithPrivateAllowlist(cfg.EgressAllowedCIDRs),
		),
	)
	if err != nil {
		return fmt.Errorf("start browser fetcher: %w", err)
	}
	defer closeBrowser()

	if err := runCrawlerService(ctx, cfg, fetcher); err != nil {
		return fmt.Errorf("run crawler service: %w", err)
	}
	return nil
}
