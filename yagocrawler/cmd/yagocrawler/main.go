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

	"github.com/D4rk4/yago/yagocrawler/internal/firefoxfetch"
	"github.com/D4rk4/yago/yagoegress"
)

var exitProcess = os.Exit

var loadCrawlerServiceConfig = LoadServiceConfig

var notifyProcessContext = signal.NotifyContext

var runConfiguredCrawler = run

var newCrawlerBrowserFetcher = firefoxfetch.NewBrowserPageFetcher

var runCrawlerService = RunService

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

// printVersion answers `yagocrawler --version` (also -version/version) by
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
	cfg.WorkerID = instanceWorkerID(cfg.WorkerID)
	crawl := cfg.Crawl
	if firefoxfetch.LooksLikeChromium(crawl.BrowserPath) {
		slog.WarnContext(
			ctx,
			"ignoring a non-Firefox browser path; the crawler slow path needs Firefox over Marionette",
			slog.String("browserPath", crawl.BrowserPath),
			slog.String("env", "YAGOCRAWLER_BROWSER_PATH"),
			slog.String(
				"action",
				"discovering Firefox on PATH; set it to firefox-esr or leave it empty",
			),
		)
	}
	fetcher, closeBrowser, err := newCrawlerBrowserFetcher(
		firefoxfetch.BrowserLaunch{
			UserAgent:        crawl.UserAgent,
			Timeout:          crawl.RequestTimeout,
			MaxBytes:         crawl.MaxBodyBytes,
			ExecPath:         crawl.BrowserPath,
			Sandbox:          crawl.BrowserSandbox,
			FailureThreshold: crawl.BrowserFailureThreshold,
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
