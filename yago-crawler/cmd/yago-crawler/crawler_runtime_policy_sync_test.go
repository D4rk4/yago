package main

import (
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
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
	reservedFree := uint64(2 << 30)
	hysteresis := uint64(384 << 20)
	message.StorageReservedFreeBytes = &reservedFree
	message.StoragePressureHysteresisBytes = &hysteresis
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
	if resolved.StorageReservedFreeBytes != reservedFree ||
		resolved.StoragePressureHysteresisBytes != hysteresis {
		t.Fatalf("resolved storage policy = %+v", resolved)
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
	message.StorageReservedFreeBytes = nil
	message.StoragePressureHysteresisBytes = nil
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		return &fakeExchange{runtimePolicy: message}, io.NopCloser(nil), nil
	}
	config := minimalServiceConfig(t)
	config.Crawl.BrowserSandbox = true
	config.Crawl.BrowserPath = "/usr/bin/firefox-esr"
	config.MetricsAddr = "127.0.0.1:9100"
	config.StorageReservedFreeBytes = 3 << 30
	config.StoragePressureHysteresisBytes = 512 << 20
	resolved, err := readCrawlerRuntimePolicy(t.Context(), config)
	if err != nil {
		t.Fatalf("read legacy policy: %v", err)
	}
	if !resolved.Crawl.BrowserSandbox ||
		resolved.Crawl.BrowserPath != config.Crawl.BrowserPath ||
		resolved.MetricsAddr != config.MetricsAddr ||
		resolved.StorageReservedFreeBytes != config.StorageReservedFreeBytes ||
		resolved.StoragePressureHysteresisBytes !=
			config.StoragePressureHysteresisBytes {
		t.Fatalf("legacy node erased crawler bootstrap facilities: %+v", resolved)
	}
}

func TestReadCrawlerRuntimePolicyControlsStartupCompactionAdmission(t *testing.T) {
	restoreAssemblySeams(t)
	config := minimalServiceConfig(t)
	config.FrontierStateMaximumBytes = 1
	config.StorageReservedFreeBytes = math.MaxUint64
	config.StoragePressureHysteresisBytes = math.MaxUint64
	path := filepath.Join(config.DataDir, "crawler", "frontier-v1.db")
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("create frontier checkpoint: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close frontier checkpoint: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat frontier checkpoint: %v", err)
	}
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	policy.FrontierStateMaximumBytes = config.FrontierStateMaximumBytes
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(policy)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	zero := uint64(0)
	message.StorageReservedFreeBytes = &zero
	message.StoragePressureHysteresisBytes = &zero
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		return &fakeExchange{runtimePolicy: message}, io.NopCloser(nil), nil
	}
	resolved, err := readCrawlerRuntimePolicy(t.Context(), config)
	if err != nil {
		t.Fatalf("read startup storage policy: %v", err)
	}
	metrics := crawlermetrics.New()
	storage := newCrawlerStorageAdmission(resolved, metrics)
	if policy := storage.Policy(); policy.ReservedFreeBytes != 0 ||
		policy.RecoveryHysteresisBytes != 0 {
		t.Fatalf("startup storage policy = %+v", policy)
	}
	session, err := openCrawlerCheckpointSession(t.Context(), resolved, storage)
	if err != nil {
		t.Fatalf("open crawler checkpoint session: %v", err)
	}
	defer func() { _ = session.checkpoint.Close() }()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat compacted frontier checkpoint: %v", err)
	}
	if os.SameFile(before, after) {
		t.Fatal("Admin startup storage policy did not admit frontier compaction")
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
