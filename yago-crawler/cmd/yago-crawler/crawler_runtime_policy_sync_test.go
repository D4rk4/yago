package main

import (
	"errors"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestReadCrawlerRuntimePolicyOverridesBootstrapBeforeAssembly(t *testing.T) {
	restoreAssemblySeams(t)
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	policy.MaximumDepth = 7
	policy.MaximumHostConcurrency = 5
	policy.BrowserPath = "/opt/firefox/firefox"
	policy.MetricsAddress = "127.0.0.1:9101"
	policy.UserAgent = "policy-agent"
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(policy)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	closed := make(chan struct{})
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		return &fakeExchange{runtimePolicy: message}, closeRecorder{closed: closed}, nil
	}
	config := minimalServiceConfig(t)
	resolved, err := readCrawlerRuntimePolicy(t.Context(), config)
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}
	if !resolved.runtimePolicy().Equal(policy) {
		t.Fatalf("resolved policy = %+v, want %+v", resolved.runtimePolicy(), policy)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("policy exchange was not closed")
	}
}

func TestReadLegacyCrawlerRuntimePolicyPreservesSandboxBootstrap(t *testing.T) {
	restoreAssemblySeams(t)
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(policy)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	message.BrowserSandbox = nil
	message.BrowserPath = nil
	message.MetricsAddress = nil
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		return &fakeExchange{runtimePolicy: message}, io.NopCloser(nil), nil
	}
	config := minimalServiceConfig(t)
	config.Crawl.BrowserSandbox = true
	config.Crawl.BrowserPath = "/usr/bin/firefox-esr"
	config.MetricsAddr = "127.0.0.1:9100"
	resolved, err := readCrawlerRuntimePolicy(t.Context(), config)
	if err != nil {
		t.Fatalf("read legacy policy: %v", err)
	}
	if !resolved.Crawl.BrowserSandbox ||
		resolved.Crawl.BrowserPath != config.Crawl.BrowserPath ||
		resolved.MetricsAddr != config.MetricsAddr {
		t.Fatalf("legacy node erased crawler bootstrap facilities: %+v", resolved)
	}
}

func TestReadCrawlerRuntimePolicyFallsBackForLegacyNode(t *testing.T) {
	restoreAssemblySeams(t)
	config := minimalServiceConfig(t)
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		return &fakeExchange{runtimeError: status.Error(codes.Unimplemented, "legacy")},
			io.NopCloser(nil), nil
	}
	resolved, err := readCrawlerRuntimePolicy(t.Context(), config)
	if err != nil || !resolved.runtimePolicy().Equal(config.runtimePolicy()) {
		t.Fatalf("legacy policy resolution = %+v/%v", resolved, err)
	}
}

func TestReadCrawlerRuntimePolicyRejectsTransportAndPayloadFailures(t *testing.T) {
	restoreAssemblySeams(t)
	config := minimalServiceConfig(t)
	sentinel := errors.New("unavailable")
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		return nil, nil, sentinel
	}
	if _, err := readCrawlerRuntimePolicy(t.Context(), config); !errors.Is(err, sentinel) {
		t.Fatalf("exchange error = %v", err)
	}
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		return &fakeExchange{runtimeError: sentinel}, io.NopCloser(nil), nil
	}
	if _, err := readCrawlerRuntimePolicy(t.Context(), config); !errors.Is(err, sentinel) {
		t.Fatalf("read error = %v", err)
	}
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		return &fakeExchange{runtimePolicy: &crawlrpc.CrawlerRuntimePolicy{}},
			io.NopCloser(nil), nil
	}
	if _, err := readCrawlerRuntimePolicy(t.Context(), config); err == nil {
		t.Fatal("invalid runtime policy accepted")
	}
}

func TestCrawlerRuntimePolicyChangeRequestsRestart(t *testing.T) {
	effective := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	restarts := 0
	apply := restartOnCrawlerRuntimePolicyChange(effective, func() { restarts++ })
	apply(effective)
	changed := effective
	changed.CrawlDelay = 2 * time.Second
	apply(changed)
	if restarts != 1 {
		t.Fatalf("restart requests = %d, want 1", restarts)
	}
	restartOnCrawlerRuntimePolicyChange(effective, nil)(changed)
}
