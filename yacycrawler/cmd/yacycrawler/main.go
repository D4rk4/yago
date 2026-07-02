package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/D4rk4/yago/yacycrawler/internal/chromedpfetch"
	"github.com/D4rk4/yago/yacyegress"
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

	if err := runConfiguredCrawler(ctx, cfg); err != nil {
		slog.ErrorContext(ctx, "crawler failed", slog.Any("error", err))
		return 1
	}
	return 0
}

func run(ctx context.Context, cfg ServiceConfig) error {
	crawl := cfg.Crawl
	fetcher, closeBrowser, err := newCrawlerBrowserFetcher(
		crawl.UserAgent,
		yacyegress.NewGuard(cfg.EgressAllowLAN),
		crawl.RequestTimeout,
		crawl.MaxBodyBytes,
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
