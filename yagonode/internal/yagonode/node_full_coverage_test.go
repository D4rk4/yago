package yagonode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/portaltheme"
	"github.com/D4rk4/yago/yagonode/internal/rankingprofile"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/snippetfetch"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// failingWriter fails every write so the export encoders' sink-error paths become
// reachable.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

// failAfterWriter accepts ok successful writes before failing, so the csv
// encoder's header flush can succeed while a later row write or flush fails.
type failAfterWriter struct{ ok int }

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if w.ok <= 0 {
		return 0, errors.New("write failed")
	}
	w.ok--

	return len(p), nil
}

// bareDenylistStore satisfies denylistStore but not denylistSnapshotter, so the
// blacklist probe's unsupported-store branch is reachable.
type bareDenylistStore struct{}

func (bareDenylistStore) Entries(context.Context) ([]urldenylist.Entry, error) {
	return nil, nil
}

func (bareDenylistStore) Add(context.Context, urldenylist.Kind, string) error {
	return nil
}

func (bareDenylistStore) Remove(context.Context, urldenylist.Kind, string) (bool, error) {
	return false, nil
}

// failingDispatch rejects every crawl start so the schedule loop's dispatch-error
// branch is reachable.
type failingDispatch struct{}

func (failingDispatch) Start(
	context.Context,
	adminui.CrawlStart,
) (adminui.CrawlDispatch, error) {
	return adminui.CrawlDispatch{}, errors.New("dispatch failed")
}

func settingByKey(t *testing.T, defs []settingDefinition, key string) settingDefinition {
	t.Helper()
	for _, def := range defs {
		if def.key == key {
			return def
		}
	}
	t.Fatalf("setting %q not found in catalog slice", key)

	return settingDefinition{}
}

func TestRuntimeTogglesNilReceiverAndAutosplit(t *testing.T) {
	t.Parallel()

	var nilToggles *runtimeToggles
	if nilToggles.CompactionInterval() != 0 {
		t.Fatal("a nil toggles must report no compaction cadence")
	}
	if nilToggles.AutosplitEnabled() {
		t.Fatal("a nil toggles must report autosplit off")
	}
	nilToggles.SetAutosplitEnabled(true)   // no-op, must not panic
	nilToggles.ApplyStorageQuota(1 << 30)  // no-op, must not panic
	if nilToggles.PortalGreeting() != "" { // nil path
		t.Fatal("a nil toggles must report no greeting")
	}

	toggles := &runtimeToggles{}
	toggles.SetAutosplitEnabled(true)
	if !toggles.AutosplitEnabled() {
		t.Fatal("autosplit toggle did not stick")
	}
	var quota int64
	toggles.SetQuotaSink(func(q int64) { quota = q })
	toggles.ApplyStorageQuota(2 << 30)
	if quota != 2<<30 {
		t.Fatalf("quota sink got %d, want %d", quota, int64(2)<<30)
	}
}

func TestPortalThemeAdminSurfacesStoreErrors(t *testing.T) {
	t.Parallel()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	theme, err := portaltheme.Open(v, themeEventSink(nil))
	if err != nil {
		t.Fatalf("open theme: %v", err)
	}
	admin := newPortalThemeAdmin(theme)
	if err := v.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}
	ctx := context.Background()

	if err := admin.SetEnabled(ctx, true); err == nil {
		t.Fatal("SetEnabled must surface the store error")
	}
	if _, err := admin.ResetDocument(ctx, portaltheme.PageSearch); err == nil {
		t.Fatal("ResetDocument must surface the store error")
	}
}

func TestCompactOnceLogsCompactError(t *testing.T) {
	t.Parallel()
	// A failing compactor takes the error path without panicking.
	compactOnce(context.Background(), &stubCompactor{err: errors.New("boom")})
}

func TestCompactorSourceSurfacesCompactError(t *testing.T) {
	t.Parallel()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	source := newCompactorSource(v)
	if err := v.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}
	if _, err := source.Compact(context.Background()); err == nil {
		t.Fatal("Compact must surface the closed-vault error")
	}
}

func TestRankingConsoleApplySurfacesPersistError(t *testing.T) {
	t.Parallel()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	holder, err := rankingprofile.Open(context.Background(), v)
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}
	source := newRankingConsole(holder, fakeRanker{}, fakeCurated{})

	if err := source.Apply(context.Background(), map[string]float64{"title": 5}); err == nil {
		t.Fatal("Apply must surface the persist error")
	}
}

func TestSpellSuggestionNilCorrectorResult(t *testing.T) {
	t.Parallel()
	searcher := recoveringSearcher{
		corrector: func() *spellcheck.Corrector { return nil },
	}
	if got := searcher.spellSuggestion([]string{"term"}); got != "" {
		t.Fatalf("a nil corrector must yield no suggestion, got %q", got)
	}
}

func TestDiscoveredSeedRejectsUnparseableHost(t *testing.T) {
	t.Parallel()
	// A valid hash with an unparseable host must not yield a seed.
	if _, ok := discoveredSeed("bad host", 8091, "PeerHash0002"); ok {
		t.Fatal("an unparseable host must not yield a seed")
	}
}

func TestBuildSnippetEnricherToleratesFetchError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	))
	defer server.Close()

	enricher := buildSnippetEnricher(nodeConfig{PeerSnippetFetch: true}, server.Client())
	search := snippetfetch.WithSnippetEnrichment(staticSearcher{resp: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title:   "t",
			URL:     server.URL,
			Snippet: "original",
			Source:  searchcore.SourceRemote,
		}},
	}}, enricher)

	resp, err := search.Search(t.Context(), searchcore.Request{Terms: []string{"x"}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(resp.Results))
	}
}

func TestBuildCrawlRuntimeAppliesQualityGate(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	storage, err := openNodeStorage(v, "")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}

	runtime, err := buildCrawlRuntime(
		crawlConfig{ListenAddr: "127.0.0.1:0", QualityGate: true},
		nodeIdentity(testConfig(t)),
		storage,
		v,
	)
	if err != nil {
		t.Fatalf("build crawl runtime: %v", err)
	}
	if runtime == nil {
		t.Fatal("an enabled crawl config must yield a runtime")
	}
}

func TestLoadDerivedConfigsRejectsRemainingBadEnv(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		envRemotePeerTimeout:  "notaduration",
		envPeerSnippetFetch:   "notabool",
		envSearchClickCapture: "notabool",
		envStorageAutosplit:   "notabool",
	}
	for key, bad := range cases {
		if _, err := loadDerivedConfigs(envWithBad(key, bad)); err == nil {
			t.Errorf("%s=%q: expected error", key, bad)
		}
	}
}

func TestLoadSeedCapabilitiesRejectsBadEnv(t *testing.T) {
	t.Parallel()
	for _, key := range []string{
		envAdvertiseDirect,
		envAdvertiseRemoteIndex,
		envAdvertiseRootNode,
		envAdvertiseSSL,
	} {
		if _, err := loadSeedCapabilities(envWithBad(key, "notabool")); err == nil {
			t.Errorf("%s: expected error for an invalid bool", key)
		}
	}
}

func TestRunSurfacesCrawlConfigError(t *testing.T) {
	t.Setenv(envPeerHash, "0123456789AB")
	t.Setenv(envPeerName, "node")
	t.Setenv(envAdvertiseHost, "203.0.113.1")
	t.Setenv(envDataDir, t.TempDir())
	// The node config loads, but the crawl config rejects the malformed toggle.
	t.Setenv(envIngestQualityGate, "notabool")

	if err := run(); err == nil {
		t.Fatal("run must surface the crawl config error")
	}
}

func TestAssembleNodeSurfacesStoreOpenErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		bucket vault.Name
	}{
		{"judgments", "search_judgments"},
		{"clicks", "search_clicks"},
		{"ranking models", "ranking_models"},
		{"content safety models", "content_safety_model"},
		{"peer reputation", "peer_reputation"},
		{"schedules", "crawl_schedules"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine := newCtrlEngine()
			engine.failProvision[tc.bucket] = true
			_, err := assembleNodeSurfaces(assembleSurfacesInput{
				ctx:    context.Background(),
				config: testConfig(t),
				vault:  ctrlVault(t, engine),
			})
			if err == nil {
				t.Fatalf("%s open error must surface", tc.name)
			}
		})
	}
}
