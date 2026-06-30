package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/chromedpfetch"
)

func main() {
	os.Exit(start())
}

func start() int {
	cfg, err := LoadServiceConfig(os.Getenv)
	if err != nil {
		slog.ErrorContext(context.Background(), "crawler config invalid", slog.Any("error", err))
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		slog.ErrorContext(ctx, "crawler failed", slog.Any("error", err))
		return 1
	}
	return 0
}

func run(ctx context.Context, cfg ServiceConfig) error {
	crawl := cfg.Crawl
	fetcher, closeBrowser := chromedpfetch.NewBrowserPageFetcher(
		crawl.UserAgent,
		cfg.ProxyURL.String(),
		crawl.RequestTimeout,
		crawl.MaxBodyBytes,
	)
	defer closeBrowser()

	if err := RunService(ctx, cfg, fetcher); err != nil {
		return fmt.Errorf("run crawler service: %w", err)
	}
	return nil
}
