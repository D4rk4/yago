package yagonode

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlschedule"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

func TestBlacklistPorterKeepsCachedProbeAndSurfacesStoreErrors(t *testing.T) {
	t.Parallel()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	denylist, err := urldenylist.Open(v, time.Now)
	if err != nil {
		t.Fatalf("open denylist: %v", err)
	}
	controller := newBlacklistController(denylist)
	if err := v.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}
	ctx := context.Background()

	if blocked, err := controller.BlacklistBlocks(
		ctx,
		"https://x.example/",
	); err != nil ||
		blocked {
		t.Fatalf("BlacklistBlocks = %t, %v", blocked, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if blocked, err := controller.BlacklistBlocks(
		canceled,
		"https://x.example/",
	); err != nil || blocked {
		t.Fatalf("cached BlacklistBlocks = %t, %v", blocked, err)
	}
	if _, err := controller.ExportBlacklist(ctx); err == nil {
		t.Fatal("ExportBlacklist must surface the entries error")
	}
	if _, err := controller.ImportBlacklist(ctx, "domain spam.example"); err == nil {
		t.Fatal("ImportBlacklist must surface the add error")
	}
}

func TestExportDocumentsHonorsContextCancellation(t *testing.T) {
	t.Parallel()
	exporter := newIndexExporter(exportFixture())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := exporter.ExportDocuments(
		ctx, adminui.IndexExportRequest{Format: "text"}, &strings.Builder{},
	); err == nil {
		t.Fatal("a cancelled context must abort the export")
	}
}

func TestExportRowWriterSurfacesSinkErrors(t *testing.T) {
	t.Parallel()
	doc := documentstore.Document{NormalizedURL: "https://a.example/"}
	for _, format := range []string{"text", "jsonl"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			write, _, err := exportRowWriter(format, failingWriter{})
			if err != nil {
				t.Fatalf("build writer: %v", err)
			}
			if err := write(doc); err == nil {
				t.Fatal("a failing sink must surface a write error")
			}
		})
	}
}

func TestExportJSONRowStampsIndexedAt(t *testing.T) {
	t.Parallel()
	row := exportJSONRow(documentstore.Document{
		NormalizedURL: "https://a.example/",
		IndexedAt:     time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
	})
	if row.IndexedAt != "2026-07-01T12:00:00Z" {
		t.Fatalf("indexedAt = %q", row.IndexedAt)
	}
}

func TestExportDocumentHostRejectsUnparseableURL(t *testing.T) {
	t.Parallel()
	host := exportDocumentHost(documentstore.Document{NormalizedURL: "http://foo.com/%zz"})
	if host != "" {
		t.Fatalf("bad url host = %q, want empty", host)
	}
}

func TestCSVRowWriterSurfacesSinkErrors(t *testing.T) {
	t.Parallel()
	// The eager header flush surfaces a wholly broken sink at construction.
	if _, _, err := csvRowWriter(failingWriter{}); err == nil {
		t.Fatal("a failing sink must surface the header write error")
	}

	// A sink that survives the header flush but then fails exercises the
	// row-write error path: an oversized row forces a flush mid-write.
	write, _, err := csvRowWriter(&failAfterWriter{ok: 1})
	if err != nil {
		t.Fatalf("build csv writer: %v", err)
	}
	oversized := documentstore.Document{
		NormalizedURL: "https://a.example/",
		Title:         strings.Repeat("x", 8192),
	}
	if err := write(oversized); err == nil {
		t.Fatal("an oversized row must surface the sink write error")
	}

	// A buffered small row flushed to the failing sink by finish() exercises the
	// flush error path.
	write, finish, err := csvRowWriter(&failAfterWriter{ok: 1})
	if err != nil {
		t.Fatalf("build csv writer: %v", err)
	}
	if err := write(documentstore.Document{NormalizedURL: "https://a.example/"}); err != nil {
		t.Fatalf("buffer row: %v", err)
	}
	if err := finish(); err == nil {
		t.Fatal("flushing buffered rows to a failing sink must surface the error")
	}
}

func TestCrawlScheduleSourceSurfacesStoreErrors(t *testing.T) {
	t.Parallel()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	store, err := crawlschedule.Open(v, time.Now)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	source := newCrawlScheduleSource(store, &recordingDispatch{})
	ctx := context.Background()

	// A blank name parses its interval but the store rejects it before storing.
	if err := source.CreateSchedule(ctx, adminui.CrawlScheduleRequest{
		Name: "  ", Seeds: []string{"https://x.example"}, Interval: "24h",
	}); err == nil {
		t.Fatal("an empty schedule name must be rejected by the store")
	}

	if err := v.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}
	if views := source.Schedules(ctx); views != nil {
		t.Fatalf("a failed list must degrade to nil, got %+v", views)
	}
	if err := source.DeleteSchedule(ctx, "missing"); err == nil {
		t.Fatal("DeleteSchedule must surface the store error")
	}
	if err := source.SetScheduleEnabled(ctx, "missing", false); err == nil {
		t.Fatal("SetScheduleEnabled must surface the store error")
	}
}

func TestFormatScheduleRunRendersTimestamp(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 1, 15, 4, 0, 0, time.UTC)
	if got := formatScheduleRun(at); got != "2026-07-01 15:04" {
		t.Fatalf("formatScheduleRun = %q", got)
	}
	if got := formatScheduleRun(time.Time{}); got != "never" {
		t.Fatalf("zero time = %q, want never", got)
	}
}

func TestDispatchDueSchedulesSurfacesListError(t *testing.T) {
	t.Parallel()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	store, err := crawlschedule.Open(v, time.Now)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}
	// A failed listing logs and returns without dispatching (no panic).
	dispatchDueSchedules(context.Background(), store, &recordingDispatch{})
}

func TestDispatchDueSchedulesLogsDispatchError(t *testing.T) {
	t.Parallel()
	store := scheduleFixture(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, crawlschedule.Schedule{
		Name: "Docs", Seeds: []string{"https://docs.example"},
		Scope: "domain", MaxDepth: 1, Interval: 24 * time.Hour,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The dispatcher rejects the due schedule; it is still marked ran.
	dispatchDueSchedules(ctx, store, failingDispatch{})

	due, err := store.DueSchedules(ctx)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("a rejected dispatch must still defer the schedule, %d still due", len(due))
	}
}

func TestDispatchDueSchedulesLogsMarkRanError(t *testing.T) {
	t.Parallel()
	engine := newCtrlEngine()
	store, err := crawlschedule.Open(ctrlVault(t, engine), time.Now)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ctx := context.Background()
	if _, err := store.Create(ctx, crawlschedule.Schedule{
		Name: "Docs", Seeds: []string{"https://docs.example"},
		Scope: "domain", MaxDepth: 1, Interval: 24 * time.Hour,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The due listing (a read view) still succeeds, but the post-dispatch
	// MarkRan write fails, exercising the mark-ran error path.
	engine.failUpdate = true
	dispatchDueSchedules(ctx, store, &recordingDispatch{})
}

func TestStorageAutosplitSettingRoundTrips(t *testing.T) {
	t.Parallel()
	def := settingByKey(t, storageAndAccessDefinitions(), "storage.autosplit")

	if got := def.defaultValue(nodeConfig{StorageAutosplit: true}); got != settingBoolTrue {
		t.Fatalf("default = %q, want %q", got, settingBoolTrue)
	}
	if applied := def.apply(nodeConfig{}, settingBoolTrue); !applied.StorageAutosplit {
		t.Fatal("apply(true) did not enable autosplit")
	}
	toggles := &runtimeToggles{}
	def.applyLive(toggles, settingBoolTrue)
	if !toggles.AutosplitEnabled() {
		t.Fatal("applyLive(true) did not flip the live toggle")
	}
}

func TestExtendedGrowthApplyClosures(t *testing.T) {
	t.Parallel()
	defs := extendedGrowthDefinitions()
	cases := []struct {
		key   string
		check func(nodeConfig) bool
	}{
		{"crawl.ingest.quality_gate", func(c nodeConfig) bool { return c.Crawl.QualityGate }},
		{"search.peer.snippet_fetch", func(c nodeConfig) bool { return c.PeerSnippetFetch }},
		{"swarm.morphology.enabled", func(c nodeConfig) bool { return c.SwarmMorphology }},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			def := settingByKey(t, defs, tc.key)
			if !tc.check(def.apply(nodeConfig{}, settingBoolTrue)) {
				t.Fatalf("%s: apply(true) did not take effect", tc.key)
			}
		})
	}
}

func TestSearchSurfaceClickCaptureApply(t *testing.T) {
	t.Parallel()
	def := settingByKey(t, searchSurfaceDefinitions(), "search.click.capture")
	if !def.apply(nodeConfig{}, settingBoolTrue).SearchClickCapture {
		t.Fatal("apply(true) did not enable click capture")
	}
}

func TestSearchRateTenMinutesRoundTrips(t *testing.T) {
	t.Parallel()
	def := settingByKey(t, searchRateDefinitions(), "search.rate.ten_minutes")
	if got := def.defaultValue(nodeConfig{}); got != "300" {
		t.Fatalf("ten-minute default = %q, want 300", got)
	}
	if applied := def.apply(nodeConfig{}, "450"); applied.SearchRate.Per10Minutes != 450 {
		t.Fatalf("apply(450) = %d, want 450", applied.SearchRate.Per10Minutes)
	}
}

func TestCIDRAndProxyFormatters(t *testing.T) {
	t.Parallel()
	if got, err := normalizeCIDRList(""); err != nil || got != "" {
		t.Fatalf("empty CIDR list = %q %v, want empty", got, err)
	}
	prefix := netip.MustParsePrefix("10.0.0.0/8")
	if got := formatPrefixes([]netip.Prefix{prefix}); got != "10.0.0.0/8" {
		t.Fatalf("formatPrefixes = %q", got)
	}
	_, network, err := net.ParseCIDR("192.168.0.0/16")
	if err != nil {
		t.Fatalf("parse cidr: %v", err)
	}
	if got := formatIPNets([]*net.IPNet{network}); got != "192.168.0.0/16" {
		t.Fatalf("formatIPNets = %q", got)
	}
}

func swapScheduleTicks(ch <-chan time.Time) func() {
	prev := newScheduleTicks
	newScheduleTicks = func() (<-chan time.Time, func()) { return ch, func() {} }

	return func() { newScheduleTicks = prev }
}

func TestRunCrawlScheduleLoopDispatchesOnTick(t *testing.T) {
	ticks := make(chan time.Time)
	defer swapScheduleTicks(ticks)()

	store := scheduleFixture(t)
	dispatch := &recordingDispatch{}
	ctx := context.Background()
	if _, err := store.Create(ctx, crawlschedule.Schedule{
		Name: "Docs", Seeds: []string{"https://docs.example"},
		Scope: "domain", MaxDepth: 1, Interval: 24 * time.Hour,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		runCrawlScheduleLoop(loopCtx, store, dispatch)
		close(done)
	}()

	// The first tick dispatches the due schedule; the second unblocks only once
	// the loop finished handling the first, a happens-before edge for the read.
	ticks <- time.Now()
	ticks <- time.Now()

	cancel()
	<-done

	if got := len(dispatch.starts); got != 1 {
		t.Fatalf("scheduled crawl dispatched %d times, want 1", got)
	}
}
