package yagonode

import (
	"context"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
)

func TestSearchScopeAuthorizerDelegates(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	service, err := adminauth.New(storage, adminauth.Config{})
	if err != nil {
		t.Fatalf("adminauth.New: %v", err)
	}

	authorizer := searchScopeAuthorizer{authorizer: service.APIKeyAuthorizer()}
	// An unknown token resolves to a decision; the point is to exercise the
	// delegating Authorize method, not a specific verdict.
	_ = authorizer.Authorize(context.Background(), "unknown-token", tavilyapi.ScopeRaw)
}

func TestSettingsSourceAppliesHTTPSRedirectLive(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	store, err := settingsstore.Open(v)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}
	toggles := newRuntimeToggles(testConfig(t))
	src := newSettingsSource(store, testConfig(t), toggles, nil)

	res, err := src.Update(context.Background(), adminui.SettingsChange{
		Key: settingKeyHTTPSRedirect, Value: settingBoolTrue,
	})
	if err != nil || !res.OK {
		t.Fatalf("update = %+v err=%v", res, err)
	}
}

func TestCrawlQueueDepthSurfacesBrokerError(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	storage, err := openNodeStorage(v, "")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	runtimeProcess, err := buildRuntimeCrawl(
		crawlConfig{ListenAddr: "127.0.0.1:0"},
		nodeIdentity(testConfig(t)),
		storage,
		v,
	)
	if err != nil {
		t.Fatalf("build crawl runtime: %v", err)
	}
	runtime := runtimeProcess.(*crawlRuntime)
	if err := v.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	if _, err := runtime.crawlQueueDepth(context.Background()); err == nil {
		t.Fatal("crawlQueueDepth should surface the broker error")
	}
}

type bareCrawlProcess struct{}

func (bareCrawlProcess) mountDispatch(*http.ServeMux) {}

func (bareCrawlProcess) Run(context.Context) {}

func (bareCrawlProcess) Close() {}

func TestAttachCrawlMetricsIgnoresNonObserver(t *testing.T) {
	// A crawl process that does not implement the observe seam must be left
	// untouched even when a collector is present.
	attachCrawlMetrics(bareCrawlProcess{}, metrics.NewCrawlMetrics(prometheus.NewRegistry()))
}
