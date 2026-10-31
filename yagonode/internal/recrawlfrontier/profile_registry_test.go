package recrawlfrontier

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func profileWithRecrawl(name string, recrawl time.Duration) yagocrawlcontract.CrawlProfile {
	return yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:            name,
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		RecrawlIfOlder:  recrawl,
	})
}

func TestRecordProfileRoundTrips(t *testing.T) {
	f := openTestFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("Example", time.Hour)
	if err := f.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}

	got, found, err := f.ProfileByHandle(ctx, profile.Handle)
	if err != nil {
		t.Fatalf("profile by handle: %v", err)
	}
	if !found {
		t.Fatal("recorded profile not found")
	}
	if got.Handle != profile.Handle || got.RecrawlIfOlder != time.Hour || got.Name != "Example" {
		t.Fatalf("profile = %+v, want handle %s recrawl 1h", got, profile.Handle)
	}
}

func TestOwnsProfileReflectsRegistry(t *testing.T) {
	f := openTestFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("Example", time.Hour)

	owns, err := f.OwnsProfile(ctx, profile.Handle)
	if err != nil {
		t.Fatalf("owns before record: %v", err)
	}
	if owns {
		t.Fatal("owns an unrecorded profile")
	}

	if err := f.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	owns, err = f.OwnsProfile(ctx, profile.Handle)
	if err != nil {
		t.Fatalf("owns after record: %v", err)
	}
	if !owns {
		t.Fatal("does not own a recorded profile")
	}
}

func TestProfileByHandleMissingReturnsNotFound(t *testing.T) {
	f := openTestFrontier(t)
	_, found, err := f.ProfileByHandle(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("profile by handle: %v", err)
	}
	if found {
		t.Fatal("unknown handle reported found")
	}
}

func TestScheduleAtIfAbsentKeepsImportedWorkOutOfFrontierUntilDue(t *testing.T) {
	f := openTestFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("migration one-shot", 0)
	if err := f.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record import profile: %v", err)
	}
	nextVisit := testBase.Add(time.Hour)
	created, err := f.ScheduleAtIfAbsent(
		ctx,
		"https://imported.example/page",
		profile.Handle,
		nextVisit,
		time.Hour,
	)
	if err != nil || !created {
		t.Fatalf("schedule imported URL = %t, %v", created, err)
	}
	if due := claim(t, f, testBase, 10); len(due) != 0 {
		t.Fatalf("future imported URL entered due frontier: %+v", due)
	}
	created, err = f.ScheduleAtIfAbsent(
		ctx,
		"https://imported.example/page",
		profile.Handle,
		testBase.Add(2*time.Hour),
		time.Hour,
	)
	if err != nil || created {
		t.Fatalf("duplicate schedule changed existing state: created=%t err=%v", created, err)
	}
	due := claim(t, f, nextVisit, 10)
	if len(due) != 1 || due[0].URL != "https://imported.example/page" {
		t.Fatalf("due imported URLs = %+v", due)
	}
}

func TestScheduleAtIfAbsentRejectsInvalidAndUnknownSchedules(t *testing.T) {
	f := openTestFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("migration schedule", 0)
	if err := f.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	tests := []struct {
		name          string
		url           string
		handle        string
		nextDueAt     time.Time
		retryInterval time.Duration
	}{
		{
			name:          "empty URL",
			handle:        profile.Handle,
			nextDueAt:     testBase.Add(time.Hour),
			retryInterval: time.Hour,
		},
		{
			name:          "empty profile",
			url:           "https://schedule.example/",
			nextDueAt:     testBase.Add(time.Hour),
			retryInterval: time.Hour,
		},
		{
			name:          "zero due time",
			url:           "https://schedule.example/",
			handle:        profile.Handle,
			retryInterval: time.Hour,
		},
		{
			name:      "zero retry interval",
			url:       "https://schedule.example/",
			handle:    profile.Handle,
			nextDueAt: testBase.Add(time.Hour),
		},
		{
			name:          "negative retry interval",
			url:           "https://schedule.example/",
			handle:        profile.Handle,
			nextDueAt:     testBase.Add(time.Hour),
			retryInterval: -time.Second,
		},
		{
			name:          "unknown profile",
			url:           "https://schedule.example/",
			handle:        "unknown",
			nextDueAt:     testBase.Add(time.Hour),
			retryInterval: time.Hour,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			created, err := f.ScheduleAtIfAbsent(
				ctx, test.url, test.handle, test.nextDueAt, test.retryInterval,
			)
			if err == nil || created {
				t.Fatalf("ScheduleAtIfAbsent = %t, %v; want rejected schedule", created, err)
			}
		})
	}
}

func TestScheduleRecrawlsAtIfAbsentWritesOneIdempotentBatch(t *testing.T) {
	f := openTestFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("migration schedule batch", 0)
	recordImportedScheduleProfile(t, f, profile)
	firstDue := testBase.Add(time.Hour)
	secondDue := testBase.Add(2 * time.Hour)
	if err := f.ScheduleRecrawlsAtIfAbsent(
		ctx,
		profile.Handle,
		[]RecrawlSchedule{
			{URL: "https://first.example/page", DueAt: firstDue},
			{URL: "https://second.example/page", DueAt: secondDue},
			{URL: "https://first.example/page", DueAt: testBase.Add(3 * time.Hour)},
		},
		time.Hour,
	); err != nil {
		t.Fatalf("schedule recrawl batch: %v", err)
	}
	if err := f.ScheduleRecrawlsAtIfAbsent(
		ctx,
		profile.Handle,
		[]RecrawlSchedule{{URL: "https://first.example/page", DueAt: testBase.Add(4 * time.Hour)}},
		time.Hour,
	); err != nil {
		t.Fatalf("repeat recrawl batch: %v", err)
	}
	first := importedScheduleRecord(t, f, "https://first.example/page")
	if !first.NextDueAt.Equal(firstDue) {
		t.Fatalf("first schedule due at %s, want %s", first.NextDueAt, firstDue)
	}
	second := importedScheduleRecord(t, f, "https://second.example/page")
	if !second.NextDueAt.Equal(secondDue) {
		t.Fatalf("second schedule due at %s, want %s", second.NextDueAt, secondDue)
	}
}

func TestScheduleRecrawlsAtIfAbsentRejectsInvalidBatchWithoutPartialWrites(t *testing.T) {
	f := openTestFrontier(t)
	profile := profileWithRecrawl("migration invalid schedule batch", 0)
	recordImportedScheduleProfile(t, f, profile)
	err := f.ScheduleRecrawlsAtIfAbsent(
		context.Background(),
		profile.Handle,
		[]RecrawlSchedule{
			{URL: "https://valid.example/page", DueAt: testBase.Add(time.Hour)},
			{URL: "", DueAt: testBase.Add(time.Hour)},
		},
		time.Hour,
	)
	if err == nil {
		t.Fatal("invalid recrawl batch was accepted")
	}
	if _, found := importedScheduleRecordFound(t, f, "https://valid.example/page"); found {
		t.Fatal("invalid batch partially wrote a valid schedule")
	}
}

func TestScheduleRecrawlsAtIfAbsentRejectsInvalidProfileTimeAndClosedStore(t *testing.T) {
	f := openTestFrontier(t)
	ctx := context.Background()
	if err := f.ScheduleRecrawlsAtIfAbsent(ctx, "p", nil, time.Hour); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	valid := []RecrawlSchedule{{URL: "https://valid.example/page", DueAt: testBase.Add(time.Hour)}}
	for _, fixture := range []struct {
		profile   string
		schedules []RecrawlSchedule
		interval  time.Duration
	}{
		{"", valid, time.Hour},
		{"p", valid, 0},
		{"p", valid, -time.Second},
		{"p", []RecrawlSchedule{{URL: "", DueAt: testBase}}, time.Hour},
		{"p", []RecrawlSchedule{{URL: "https://valid.example/page"}}, time.Hour},
	} {
		if err := f.ScheduleRecrawlsAtIfAbsent(
			ctx, fixture.profile, fixture.schedules, fixture.interval,
		); err == nil {
			t.Errorf("invalid batch profile=%q schedules=%+v interval=%s was accepted",
				fixture.profile, fixture.schedules, fixture.interval)
		}
	}
	if err := f.ScheduleRecrawlsAtIfAbsent(ctx, "unknown", valid, time.Hour); err == nil {
		t.Fatal("unknown profile was accepted")
	}
	if err := f.vault.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.ScheduleRecrawlsAtIfAbsent(ctx, "p", valid, time.Hour); err == nil {
		t.Fatal("closed store was accepted")
	}
}

func TestScheduleRecrawlsAtIfAbsentSurfacesReadAndWriteFailures(t *testing.T) {
	url := "https://schedule-batch.example/imported"
	profile := profileWithRecrawl("migration batch error", 0)
	tests := []struct {
		name  string
		setup func(*testing.T, *Frontier, *ctrlEngine)
	}{
		{
			name: "profile read",
			setup: func(_ *testing.T, _ *Frontier, engine *ctrlEngine) {
				engine.seed(profileBucket, profile.Handle, []byte("{"))
			},
		},
		{
			name: "record read",
			setup: func(t *testing.T, frontier *Frontier, engine *ctrlEngine) {
				recordImportedScheduleProfile(t, frontier, profile)
				engine.seed(recordBucket, hashKey(t, url), []byte("{"))
			},
		},
		{
			name: "record write",
			setup: func(t *testing.T, frontier *Frontier, engine *ctrlEngine) {
				recordImportedScheduleProfile(t, frontier, profile)
				engine.failPut[recordBucket] = true
			},
		},
		{
			name: "due write",
			setup: func(t *testing.T, frontier *Frontier, engine *ctrlEngine) {
				recordImportedScheduleProfile(t, frontier, profile)
				engine.failPut[dueBucket] = true
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frontier, engine := openCtrlFrontier(t)
			test.setup(t, frontier, engine)
			err := frontier.ScheduleRecrawlsAtIfAbsent(
				context.Background(), profile.Handle,
				[]RecrawlSchedule{{URL: url, DueAt: testBase.Add(time.Hour)}},
				time.Hour,
			)
			if err == nil {
				t.Fatal("recrawl batch storage failure was ignored")
			}
		})
	}
}

func importedScheduleRecord(t *testing.T, f *Frontier, url string) scheduleRecord {
	t.Helper()
	record, found := importedScheduleRecordFound(t, f, url)
	if !found {
		t.Fatalf("schedule for %s was not stored", url)
	}
	return record
}

func importedScheduleRecordFound(
	t *testing.T,
	f *Frontier,
	url string,
) (scheduleRecord, bool) {
	t.Helper()
	hash, err := yagomodel.HashURL(url)
	if err != nil {
		t.Fatalf("hash URL: %v", err)
	}
	var (
		record scheduleRecord
		found  bool
	)
	if err := f.vault.View(t.Context(), func(tx *vault.Txn) error {
		var readErr error
		record, found, readErr = f.records.Get(tx, vault.Key(string(hash)))
		if readErr != nil {
			return fmt.Errorf("read imported schedule: %w", readErr)
		}
		return nil
	}); err != nil {
		t.Fatalf("read scheduled record: %v", err)
	}
	return record, found
}

func TestScheduleAtIfAbsentSurfacesCorruptProfileAndRecord(t *testing.T) {
	profile := profileWithRecrawl("migration schedule", 0)
	url := "https://schedule.example/imported"
	t.Run("profile read", func(t *testing.T) {
		frontier, engine := openCtrlFrontier(t)
		engine.seed(profileBucket, profile.Handle, []byte("{"))
		assertImportedScheduleFails(t, frontier, url, profile.Handle, context.Background())
	})
	t.Run("existing record read", func(t *testing.T) {
		frontier, engine := openCtrlFrontier(t)
		recordImportedScheduleProfile(t, frontier, profile)
		engine.seed(recordBucket, hashKey(t, url), []byte("{"))
		assertImportedScheduleFails(t, frontier, url, profile.Handle, context.Background())
	})
}

func TestScheduleAtIfAbsentSurfacesCanceledProfileRead(t *testing.T) {
	frontier, _ := openCtrlFrontier(t)
	profile := profileWithRecrawl("migration schedule", 0)
	recordImportedScheduleProfile(t, frontier, profile)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assertImportedScheduleFails(
		t,
		frontier,
		"https://schedule.example/imported",
		profile.Handle,
		ctx,
	)
}

func TestScheduleAtIfAbsentSurfacesRecordAndDueWriteFailures(t *testing.T) {
	tests := []struct {
		name string
		fail func(*ctrlEngine)
	}{
		{name: "record", fail: func(engine *ctrlEngine) { engine.failPut[recordBucket] = true }},
		{name: "due", fail: func(engine *ctrlEngine) { engine.failPut[dueBucket] = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frontier, engine := openCtrlFrontier(t)
			profile := profileWithRecrawl("migration schedule", 0)
			recordImportedScheduleProfile(t, frontier, profile)
			test.fail(engine)
			assertImportedScheduleFails(
				t,
				frontier,
				"https://schedule.example/imported",
				profile.Handle,
				context.Background(),
			)
		})
	}
}

func recordImportedScheduleProfile(
	t *testing.T,
	frontier *Frontier,
	profile yagocrawlcontract.CrawlProfile,
) {
	t.Helper()
	if err := frontier.RecordProfile(
		t.Context(),
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}
}

func assertImportedScheduleFails(
	t *testing.T,
	frontier *Frontier,
	url, profileHandle string,
	ctx context.Context,
) {
	t.Helper()
	created, err := frontier.ScheduleAtIfAbsent(
		ctx,
		url,
		profileHandle,
		testBase.Add(time.Hour),
		time.Hour,
	)
	if err == nil || created {
		t.Fatalf("ScheduleAtIfAbsent = %t, %v; want storage failure", created, err)
	}
}

func TestImportedDueScheduleSurvivesFrontierRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "frontier.db")
	v, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	frontier, err := Open(v)
	if err != nil {
		t.Fatalf("open frontier: %v", err)
	}
	profile := profileWithRecrawl("migration-recovery", 0)
	if err := frontier.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	nextVisit := testBase.Add(2 * time.Hour)
	if _, err := frontier.ScheduleAtIfAbsent(
		ctx,
		"https://restart.example/imported",
		profile.Handle,
		nextVisit,
		time.Hour,
	); err != nil {
		t.Fatalf("schedule imported URL: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}
	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen vault: %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("close reopened vault: %v", err)
		}
	})
	restored, err := Open(reopened)
	if err != nil {
		t.Fatalf("reopen frontier: %v", err)
	}
	due, err := restored.ClaimDue(ctx, nextVisit, 10)
	if err != nil || len(due) != 1 || due[0].URL != "https://restart.example/imported" {
		t.Fatalf("restored due schedule = %+v, %v", due, err)
	}
}

func TestRecordProfileRejectsEmptyHandle(t *testing.T) {
	f := openTestFrontier(t)
	if err := f.RecordProfile(
		context.Background(),
		yagocrawlcontract.CrawlProfile{},
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err == nil {
		t.Fatal("expected error for empty handle")
	}
}

func TestRecordFetchSchedulesFromProfileInterval(t *testing.T) {
	f := openTestFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("Example", time.Hour)
	if err := f.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	if err := f.RecordFetch(ctx, "https://a.example/", profile.Handle, testBase); err != nil {
		t.Fatalf("record fetch: %v", err)
	}

	if due := claim(t, f, testBase.Add(30*time.Minute), 10); len(due) != 0 {
		t.Fatalf("claimed %d before due, want 0", len(due))
	}
	due := claim(t, f, testBase.Add(time.Hour), 10)
	if len(due) != 1 || due[0].URL != "https://a.example/" {
		t.Fatalf("claimed = %+v, want a.example due at +1h", due)
	}
}

func TestRecordFetchUnknownHandleIsNoop(t *testing.T) {
	f := openTestFrontier(t)
	if err := f.RecordFetch(
		context.Background(),
		"https://a.example/",
		"never-recorded",
		testBase,
	); err != nil {
		t.Fatalf("record fetch: %v", err)
	}
	if due := claim(t, f, testBase.Add(1000*time.Hour), 10); len(due) != 0 {
		t.Fatalf("claimed %d for unknown-profile fetch, want 0", len(due))
	}
}

func TestRecordFetchNeverRecrawlProfileDoesNotSchedule(t *testing.T) {
	f := openTestFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("NoRecrawl", 0)
	if err := f.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	if err := f.RecordFetch(ctx, "https://a.example/", profile.Handle, testBase); err != nil {
		t.Fatalf("record fetch: %v", err)
	}
	if due := claim(t, f, testBase.Add(1000*time.Hour), 10); len(due) != 0 {
		t.Fatalf("claimed %d for never-recrawl profile, want 0", len(due))
	}
}

func TestProfileRegistryPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	v, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("boltvault: %v", err)
	}
	frontier, err := Open(v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	profile := profileWithRecrawl("Example", time.Hour)
	if err := frontier.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	restored, err := Open(reopened)
	if err != nil {
		t.Fatalf("reopen frontier: %v", err)
	}
	if err := restored.RecordFetch(
		ctx,
		"https://a.example/",
		profile.Handle,
		testBase,
	); err != nil {
		t.Fatalf("record fetch after reopen: %v", err)
	}
	if due := claim(t, restored, testBase.Add(time.Hour), 10); len(due) != 1 {
		t.Fatalf("claimed %d after reopen, want 1", len(due))
	}
}

// TestRecordFetchesSchedulesKnownAndSkipsUnknown is the IO-AGG-01 acceptance:
// one call schedules a whole micro-batch — known-profile fetches become due,
// unknown handles are skipped like RecordFetch, in one transaction.
func TestRecordFetchesSchedulesKnownAndSkipsUnknown(t *testing.T) {
	f := openTestFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("Example", time.Hour)
	if err := f.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}

	err := f.RecordFetches(ctx,
		[]string{"https://a.example/", "https://b.example/"},
		[]string{profile.Handle, "never-recorded"},
		[]time.Time{testBase, testBase},
	)
	if err != nil {
		t.Fatalf("record fetches: %v", err)
	}

	due := claim(t, f, testBase.Add(time.Hour), 10)
	if len(due) != 1 || due[0].URL != "https://a.example/" {
		t.Fatalf("claimed = %+v, want only a.example due at +1h", due)
	}
}

// TestRecordFetchesDeduplicatesHandleResolution pins that a batch repeating one
// handle resolves that profile once yet still schedules every fetch under it.
func TestRecordFetchesDeduplicatesHandleResolution(t *testing.T) {
	f := openTestFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("Example", time.Hour)
	if err := f.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}

	err := f.RecordFetches(ctx,
		[]string{"https://a.example/", "https://b.example/"},
		[]string{profile.Handle, profile.Handle},
		[]time.Time{testBase, testBase},
	)
	if err != nil {
		t.Fatalf("record fetches: %v", err)
	}

	if due := claim(t, f, testBase.Add(time.Hour), 10); len(due) != 2 {
		t.Fatalf("claimed %d, want both urls of the repeated handle", len(due))
	}
}

func TestRecordFetchesProfileReadError(t *testing.T) {
	f, engine := openCtrlFrontier(t)
	engine.seed(profileBucket, "h", []byte("{corrupt"))
	err := f.RecordFetches(context.Background(),
		[]string{"https://a.example/"},
		[]string{"h"},
		[]time.Time{testBase},
	)
	if err == nil {
		t.Fatal("expected a profile read error resolving a corrupt profile")
	}
}

func TestRecordFetchesObserveError(t *testing.T) {
	f, _ := openCtrlFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("Example", 48*time.Hour)
	if err := f.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	err := f.RecordFetches(ctx,
		[]string{"https://a.example/"},
		[]string{profile.Handle},
		[]time.Time{year10000Base},
	)
	if err == nil {
		t.Fatal("expected an observe error scheduling a year-10000 due time")
	}
}

func TestRecordFetchesRejectsMismatchedLengths(t *testing.T) {
	f := openTestFrontier(t)
	if err := f.RecordFetches(
		context.Background(),
		[]string{"https://a.example/"},
		[]string{"h1", "h2"},
		[]time.Time{testBase},
	); err == nil {
		t.Fatal("mismatched slice lengths must be rejected")
	}
}

// The sweeper rebuilds a due URL's order from this record, so the priority it
// was dispatched under has to come back with the profile: the whole-run page
// budget of an automatic-discovery order is bounded by its per-host cap, and a
// recrawl re-dispatched as an ordinary order silently loses that bound.
func TestDispatchByHandleReturnsTheRecordedPriority(t *testing.T) {
	f := openTestFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("Seed", time.Hour)
	if err := f.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}

	got, priority, found, err := f.DispatchByHandle(ctx, profile.Handle)
	if err != nil {
		t.Fatalf("dispatch by handle: %v", err)
	}
	if !found || got.Handle != profile.Handle ||
		priority != yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery {
		t.Fatalf("dispatch = %+v priority %q found %v", got, priority, found)
	}

	if _, priority, found, err = f.DispatchByHandle(ctx, "absent"); err != nil ||
		found || priority != yagocrawlcontract.CrawlOrderPriorityNormal {
		t.Fatalf("absent dispatch = priority %q found %v err %v", priority, found, err)
	}
}
