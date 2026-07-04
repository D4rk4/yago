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
