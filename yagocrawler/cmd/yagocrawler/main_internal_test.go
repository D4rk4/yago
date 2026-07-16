package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yagocrawler/internal/firefoxfetch"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagoegress"
)

func restoreMainSeams(t *testing.T) {
	t.Helper()
	savedExitProcess := exitProcess
	savedLoadConfig := loadCrawlerServiceConfig
	savedNotifyContext := notifyProcessContext
	savedRunConfiguredCrawler := runConfiguredCrawler
	savedBrowserFetcher := newCrawlerBrowserFetcher
	savedRunCrawlerService := runCrawlerService
	t.Cleanup(func() {
		exitProcess = savedExitProcess
		loadCrawlerServiceConfig = savedLoadConfig
		notifyProcessContext = savedNotifyContext
		runConfiguredCrawler = savedRunConfiguredCrawler
		newCrawlerBrowserFetcher = savedBrowserFetcher
		runCrawlerService = savedRunCrawlerService
	})
}

func minimalServiceConfig(t *testing.T) ServiceConfig {
	t.Helper()
	return ServiceConfig{
		Crawl:       DefaultCrawlConfig(),
		NodeRPCAddr: "node:9091",
		WorkerID:    DefaultWorkerID,
	}
}

func TestMainExitsWithStartCode(t *testing.T) {
	restoreMainSeams(t)
	loadCrawlerServiceConfig = func(func(string) string) (ServiceConfig, error) {
		return ServiceConfig{}, errors.New("bad config")
	}
	gotCode := -1
	exitProcess = func(code int) {
		gotCode = code
		panic("exit")
	}

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("main did not exit")
		}
		if gotCode != 2 {
			t.Fatalf("exit code = %d, want 2", gotCode)
		}
	}()
	main()
}

func TestStartReturnsConfigErrorCode(t *testing.T) {
	restoreMainSeams(t)
	loadCrawlerServiceConfig = func(func(string) string) (ServiceConfig, error) {
		return ServiceConfig{}, errors.New("bad config")
	}

	if code := start(); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestStartReturnsRunErrorCode(t *testing.T) {
	restoreMainSeams(t)
	loadCrawlerServiceConfig = func(func(string) string) (ServiceConfig, error) {
		return minimalServiceConfig(t), nil
	}
	notifyProcessContext = func(ctx context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}
	runConfiguredCrawler = func(context.Context, ServiceConfig) error {
		return errors.New("run failed")
	}

	if code := start(); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
}

func TestStartReturnsSuccessCode(t *testing.T) {
	restoreMainSeams(t)
	loadCrawlerServiceConfig = func(func(string) string) (ServiceConfig, error) {
		return minimalServiceConfig(t), nil
	}
	notifyProcessContext = func(ctx context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}
	runConfiguredCrawler = func(context.Context, ServiceConfig) error {
		return nil
	}

	if code := start(); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
}

func TestRunClosesBrowserOnSuccess(t *testing.T) {
	restoreMainSeams(t)
	closed := false
	newCrawlerBrowserFetcher = func(
		_ firefoxfetch.BrowserLaunch, _ yagoegress.Guard,
		_ ...func(),
	) (*firefoxfetch.BrowserPageFetcher, func(), error) {
		return &firefoxfetch.BrowserPageFetcher{}, func() { closed = true }, nil
	}
	runCrawlerService = func(
		context.Context,
		ServiceConfig,
		pagefetch.PageSource,
		*crawlermetrics.Metrics,
	) error {
		return nil
	}

	if err := run(context.Background(), minimalServiceConfig(t)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !closed {
		t.Fatal("browser cleanup was not called")
	}
}

func TestRunWiresBrowserSlotAcquisitionDeadlineToServiceMetrics(t *testing.T) {
	restoreMainSeams(t)
	var observeBrowserSlotAcquisitionDeadline func()
	newCrawlerBrowserFetcher = func(
		_ firefoxfetch.BrowserLaunch,
		_ yagoegress.Guard,
		observers ...func(),
	) (*firefoxfetch.BrowserPageFetcher, func(), error) {
		if len(observers) != 1 || observers[0] == nil {
			t.Fatalf("browser slot acquisition observers = %d, want one", len(observers))
		}
		observeBrowserSlotAcquisitionDeadline = observers[0]

		return &firefoxfetch.BrowserPageFetcher{}, func() {}, nil
	}
	runCrawlerService = func(
		_ context.Context,
		_ ServiceConfig,
		_ pagefetch.PageSource,
		metrics *crawlermetrics.Metrics,
	) error {
		observeBrowserSlotAcquisitionDeadline()
		request := httptest.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			"/metrics",
			nil,
		)
		response := httptest.NewRecorder()
		metrics.Handler().ServeHTTP(response, request)
		body := response.Body.String()
		if !strings.Contains(
			body,
			"yacy_crawler_browser_slot_acquisition_deadlines_total 1",
		) {
			t.Fatalf("browser slot acquisition deadline metric missing:\n%s", body)
		}

		return nil
	}

	if err := run(context.Background(), minimalServiceConfig(t)); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunUsesOneUniqueWorkerIdentityPerProcessInvocation(t *testing.T) {
	restoreMainSeams(t)
	newCrawlerBrowserFetcher = func(
		_ firefoxfetch.BrowserLaunch, _ yagoegress.Guard,
		_ ...func(),
	) (*firefoxfetch.BrowserPageFetcher, func(), error) {
		return &firefoxfetch.BrowserPageFetcher{}, func() {}, nil
	}
	var workerIDs []string
	runCrawlerService = func(
		_ context.Context,
		cfg ServiceConfig,
		_ pagefetch.PageSource,
		_ *crawlermetrics.Metrics,
	) error {
		workerIDs = append(workerIDs, cfg.WorkerID)

		return nil
	}
	for range 2 {
		if err := run(context.Background(), minimalServiceConfig(t)); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	if len(workerIDs) != 2 || workerIDs[0] == workerIDs[1] {
		t.Fatalf("worker identities = %v, want two unique values", workerIDs)
	}
	for _, workerID := range workerIDs {
		if !strings.HasPrefix(workerID, DefaultWorkerID+"-") {
			t.Fatalf("worker identity = %q, want default prefix", workerID)
		}
	}
}

func TestRunClosesBrowserOnServiceError(t *testing.T) {
	restoreMainSeams(t)
	sentinel := errors.New("service failed")
	closed := false
	newCrawlerBrowserFetcher = func(
		_ firefoxfetch.BrowserLaunch, _ yagoegress.Guard,
		_ ...func(),
	) (*firefoxfetch.BrowserPageFetcher, func(), error) {
		return &firefoxfetch.BrowserPageFetcher{}, func() { closed = true }, nil
	}
	runCrawlerService = func(
		context.Context,
		ServiceConfig,
		pagefetch.PageSource,
		*crawlermetrics.Metrics,
	) error {
		return sentinel
	}

	err := run(context.Background(), minimalServiceConfig(t))
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
	if !closed {
		t.Fatal("browser cleanup was not called")
	}
}

func TestRunReturnsBrowserStartError(t *testing.T) {
	restoreMainSeams(t)
	sentinel := errors.New("browser start failed")
	newCrawlerBrowserFetcher = func(
		_ firefoxfetch.BrowserLaunch, _ yagoegress.Guard,
		_ ...func(),
	) (*firefoxfetch.BrowserPageFetcher, func(), error) {
		return nil, nil, sentinel
	}

	if err := run(context.Background(), minimalServiceConfig(t)); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestRunWarnsOnChromiumBrowserPath(t *testing.T) {
	restoreMainSeams(t)

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(
		slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})),
	)
	t.Cleanup(func() { slog.SetDefault(previous) })

	newCrawlerBrowserFetcher = func(
		_ firefoxfetch.BrowserLaunch, _ yagoegress.Guard,
		_ ...func(),
	) (*firefoxfetch.BrowserPageFetcher, func(), error) {
		return &firefoxfetch.BrowserPageFetcher{}, func() {}, nil
	}
	runCrawlerService = func(
		context.Context,
		ServiceConfig,
		pagefetch.PageSource,
		*crawlermetrics.Metrics,
	) error {
		return nil
	}

	cfg := minimalServiceConfig(t)
	cfg.Crawl.BrowserPath = "/usr/bin/chromium"
	if err := run(context.Background(), cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(logs.String(), "non-Firefox browser path") {
		t.Fatalf("want a chromium browser-path warning, got: %s", logs.String())
	}
}

func TestPrintVersionReportsBuildVersion(t *testing.T) {
	for _, arg := range []string{"--version", "-version", "version"} {
		var buf bytes.Buffer
		if !printVersion([]string{arg}, &buf) {
			t.Fatalf("printVersion(%q) = false, want true", arg)
		}
		want := "yago-crawler " + version + "\n"
		if buf.String() != want {
			t.Fatalf("printVersion(%q) wrote %q, want %q", arg, buf.String(), want)
		}
	}
}

func TestPrintVersionIgnoresOtherArgs(t *testing.T) {
	var buf bytes.Buffer
	for _, args := range [][]string{nil, {}, {"serve"}, {"-h"}} {
		if printVersion(args, &buf) {
			t.Fatalf("printVersion(%v) = true, want false", args)
		}
	}
	if buf.Len() != 0 {
		t.Fatalf("printVersion wrote %q for non-version args", buf.String())
	}
}

// TestStartPrintsVersionAndExitsZero covers start's version short-circuit: when
// os.Args carries --version, printVersion handles it and start returns 0 before
// loading any config. Stdout is redirected so the stamped version line does not
// leak into test output.
func TestStartPrintsVersionAndExitsZero(t *testing.T) {
	savedArgs := os.Args
	savedStdout := os.Stdout
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	os.Stdout = devNull
	os.Args = []string{"yagocrawler", "--version"}
	t.Cleanup(func() {
		os.Stdout = savedStdout
		os.Args = savedArgs
		_ = devNull.Close()
	})

	if code := start(); code != 0 {
		t.Fatalf("code = %d, want 0 for --version", code)
	}
}

func TestStartReturnsRestartCode(t *testing.T) {
	restoreMainSeams(t)
	loadCrawlerServiceConfig = func(func(string) string) (ServiceConfig, error) {
		return minimalServiceConfig(t), nil
	}
	notifyProcessContext = func(ctx context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}
	runConfiguredCrawler = func(context.Context, ServiceConfig) error {
		return errRestartRequested
	}

	if code := start(); code != restartExitCode {
		t.Fatalf("code = %d, want %d", code, restartExitCode)
	}
}
