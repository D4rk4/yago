package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

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
		firefoxfetch.BrowserLaunch, yagoegress.Guard,
	) (*firefoxfetch.BrowserPageFetcher, func(), error) {
		return &firefoxfetch.BrowserPageFetcher{}, func() { closed = true }, nil
	}
	runCrawlerService = func(context.Context, ServiceConfig, pagefetch.PageSource) error {
		return nil
	}

	if err := run(context.Background(), minimalServiceConfig(t)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !closed {
		t.Fatal("browser cleanup was not called")
	}
}

func TestRunClosesBrowserOnServiceError(t *testing.T) {
	restoreMainSeams(t)
	sentinel := errors.New("service failed")
	closed := false
	newCrawlerBrowserFetcher = func(
		firefoxfetch.BrowserLaunch, yagoegress.Guard,
	) (*firefoxfetch.BrowserPageFetcher, func(), error) {
		return &firefoxfetch.BrowserPageFetcher{}, func() { closed = true }, nil
	}
	runCrawlerService = func(context.Context, ServiceConfig, pagefetch.PageSource) error {
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
		firefoxfetch.BrowserLaunch, yagoegress.Guard,
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
