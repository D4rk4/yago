package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yago-crawler/internal/firefoxfetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
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
	savedSynchronizePolicy := synchronizeCrawlerRuntimePolicy
	synchronizeCrawlerRuntimePolicy = func(
		_ context.Context,
		config ServiceConfig,
	) (ServiceConfig, error) {
		return config, nil
	}
	t.Cleanup(func() {
		exitProcess = savedExitProcess
		loadCrawlerServiceConfig = savedLoadConfig
		notifyProcessContext = savedNotifyContext
		runConfiguredCrawler = savedRunConfiguredCrawler
		newCrawlerBrowserFetcher = savedBrowserFetcher
		runCrawlerService = savedRunCrawlerService
		synchronizeCrawlerRuntimePolicy = savedSynchronizePolicy
	})
}

func minimalServiceConfig(t *testing.T) ServiceConfig {
	t.Helper()
	return ServiceConfig{
		Crawl:       DefaultCrawlConfig(),
		NodeRPCAddr: "node:9091",
		DataDir:     t.TempDir(),
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
	configuredRedirects := -1
	newCrawlerBrowserFetcher = func(
		launch firefoxfetch.BrowserLaunch, _ yagoegress.Guard,
		_ firefoxfetch.BrowserPoolObserver,
	) (*firefoxfetch.BrowserPageFetcher, func(), error) {
		configuredRedirects = launch.MaxRedirects
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
	if configuredRedirects != DefaultMaxRedirects {
		t.Fatalf("browser redirect limit = %d, want %d", configuredRedirects, DefaultMaxRedirects)
	}
}

func TestRunReturnsRuntimePolicySynchronizationError(t *testing.T) {
	restoreMainSeams(t)
	sentinel := errors.New("runtime policy unavailable")
	synchronizeCrawlerRuntimePolicy = func(
		context.Context,
		ServiceConfig,
	) (ServiceConfig, error) {
		return ServiceConfig{}, sentinel
	}
	browserStarted := false
	newCrawlerBrowserFetcher = func(
		firefoxfetch.BrowserLaunch,
		yagoegress.Guard,
		firefoxfetch.BrowserPoolObserver,
	) (*firefoxfetch.BrowserPageFetcher, func(), error) {
		browserStarted = true

		return nil, nil, errors.New("unexpected browser start")
	}

	err := run(t.Context(), minimalServiceConfig(t))
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want runtime policy failure", err)
	}
	if browserStarted {
		t.Fatal("browser started before runtime policy synchronization")
	}
}

func TestRunWiresBrowserPoolObservationToServiceMetrics(t *testing.T) {
	restoreMainSeams(t)
	var observer firefoxfetch.BrowserPoolObserver
	newCrawlerBrowserFetcher = func(
		_ firefoxfetch.BrowserLaunch,
		_ yagoegress.Guard,
		configuredObserver firefoxfetch.BrowserPoolObserver,
	) (*firefoxfetch.BrowserPageFetcher, func(), error) {
		if configuredObserver == nil {
			t.Fatal("browser pool observer is nil")
		}
		observer = configuredObserver

		return &firefoxfetch.BrowserPageFetcher{}, func() {}, nil
	}
	runCrawlerService = func(
		_ context.Context,
		_ ServiceConfig,
		_ pagefetch.PageSource,
		metrics *crawlermetrics.Metrics,
	) error {
		observer.ObserveBrowserFailure(firefoxfetch.BrowserFailureSlotDeadline)
		observer.ObserveBrowserSlotWait(250 * time.Millisecond)
		observer.ObserveBrowserPoolState(firefoxfetch.BrowserPoolState{
			Ready:   1,
			Busy:    2,
			Cooling: 3,
		})
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
		for _, sample := range []string{
			`yacy_crawler_browser_failures_total{reason="slot_deadline"} 1`,
			`yacy_crawler_browser_sessions{state="ready"} 1`,
			`yacy_crawler_browser_sessions{state="busy"} 2`,
			`yacy_crawler_browser_sessions{state="cooling"} 3`,
			"yacy_crawler_browser_slot_acquisition_seconds_count 1",
		} {
			if !strings.Contains(body, sample) {
				t.Fatalf("browser pool metric %q missing:\n%s", sample, body)
			}
		}

		return nil
	}

	if err := run(context.Background(), minimalServiceConfig(t)); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunPreservesConfiguredWorkerIdentityForServiceAssembly(t *testing.T) {
	restoreMainSeams(t)
	newCrawlerBrowserFetcher = func(
		_ firefoxfetch.BrowserLaunch, _ yagoegress.Guard,
		_ firefoxfetch.BrowserPoolObserver,
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
	if len(workerIDs) != 2 || workerIDs[0] != workerIDs[1] {
		t.Fatalf("worker identities = %v, want the configured identity", workerIDs)
	}
	for _, workerID := range workerIDs {
		if workerID != DefaultWorkerID {
			t.Fatalf("worker identity = %q, want %q", workerID, DefaultWorkerID)
		}
	}
}

func TestRunClosesBrowserOnServiceError(t *testing.T) {
	restoreMainSeams(t)
	sentinel := errors.New("service failed")
	closed := false
	newCrawlerBrowserFetcher = func(
		_ firefoxfetch.BrowserLaunch, _ yagoegress.Guard,
		_ firefoxfetch.BrowserPoolObserver,
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
		_ firefoxfetch.BrowserPoolObserver,
	) (*firefoxfetch.BrowserPageFetcher, func(), error) {
		return nil, nil, sentinel
	}

	if err := run(context.Background(), minimalServiceConfig(t)); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
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
	os.Args = []string{"yago-crawler", "--version"}
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
