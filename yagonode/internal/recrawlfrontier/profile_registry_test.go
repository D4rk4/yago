package recrawlfrontier

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
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
	if err := f.RecordProfile(ctx, profile); err != nil {
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

	if err := f.RecordProfile(ctx, profile); err != nil {
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

func TestRecordProfileRejectsEmptyHandle(t *testing.T) {
	f := openTestFrontier(t)
	if err := f.RecordProfile(context.Background(), yagocrawlcontract.CrawlProfile{}); err == nil {
		t.Fatal("expected error for empty handle")
	}
}

func TestRecordFetchSchedulesFromProfileInterval(t *testing.T) {
	f := openTestFrontier(t)
	ctx := context.Background()
	profile := profileWithRecrawl("Example", time.Hour)
	if err := f.RecordProfile(ctx, profile); err != nil {
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
	if err := f.RecordProfile(ctx, profile); err != nil {
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
	if err := frontier.RecordProfile(ctx, profile); err != nil {
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
	if err := f.RecordProfile(ctx, profile); err != nil {
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
	if err := f.RecordProfile(ctx, profile); err != nil {
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
	if err := f.RecordProfile(ctx, profile); err != nil {
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
