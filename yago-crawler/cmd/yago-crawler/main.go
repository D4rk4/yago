package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yago-crawler/internal/firefoxfetch"
	"github.com/D4rk4/yago/yagoegress"
)

var exitProcess = os.Exit

var loadCrawlerServiceConfig = LoadServiceConfig

var notifyProcessContext = signal.NotifyContext

var runConfiguredCrawler = run

var newCrawlerBrowserFetcher = firefoxfetch.NewBrowserPageFetcherWithPoolObservation

var runCrawlerService = runServiceWithMetrics

func main() {
	exitProcess(start())
}

func start() int {
	if printVersion(os.Args[1:], os.Stdout) {
		return 0
	}

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

// printVersion answers `yago-crawler --version` (also -version/version) by
// writing the stamped build version, returning true when it handled the request
// so the crawler does not otherwise start.
func printVersion(args []string, out io.Writer) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "--version", "-version", "version":
		_, _ = fmt.Fprintln(out, "yago-crawler "+version)

		return true
	default:
		return false
	}
}

func run(ctx context.Context, cfg ServiceConfig) error {
	var err error
	cfg, err = synchronizeCrawlerRuntimePolicy(ctx, cfg)
	if err != nil {
		return err
	}
	crawl := cfg.Crawl
	metrics := crawlermetrics.New()
	fetcher, closeBrowser, err := newCrawlerBrowserFetcher(
		firefoxfetch.BrowserLaunch{
			UserAgent:        crawl.UserAgent,
			Timeout:          crawl.RequestTimeout,
			MaxBytes:         crawl.MaxBodyBytes,
			ExecPath:         crawl.BrowserPath,
			Sandbox:          crawl.BrowserSandbox,
			FailureThreshold: crawl.BrowserFailureThreshold,
			MaxRedirects:     crawl.MaxRedirects,
		},
		yagoegress.NewGuard(
			cfg.EgressAllowLAN,
			yagoegress.WithPrivateAllowlist(cfg.EgressAllowedCIDRs),
		),
		metrics,
	)
	if err != nil {
		return fmt.Errorf("start browser fetcher: %w", err)
	}
	defer closeBrowser()

	if err := runCrawlerService(ctx, cfg, fetcher, metrics); err != nil {
		return fmt.Errorf("run crawler service: %w", err)
	}
	return nil
}
