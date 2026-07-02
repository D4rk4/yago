package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacycrawler/internal/chromedpfetch"
	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
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
	proxyURL, err := url.Parse("http://proxy:4750")
	if err != nil {
		t.Fatalf("parse proxy: %v", err)
	}
	cfg := ServiceConfig{
		Crawl:    DefaultCrawlConfig(),
		NATSURL:  "nats://localhost:4222",
		ProxyURL: proxyURL,
	}
	return cfg
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
	newCrawlerBrowserFetcher = func(string, string, time.Duration, int64) (*chromedpfetch.BrowserPageFetcher, func()) {
		return &chromedpfetch.BrowserPageFetcher{}, func() { closed = true }
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
	newCrawlerBrowserFetcher = func(string, string, time.Duration, int64) (*chromedpfetch.BrowserPageFetcher, func()) {
		return &chromedpfetch.BrowserPageFetcher{}, func() { closed = true }
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
