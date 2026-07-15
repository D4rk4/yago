package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlschedule"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

type recordingDispatch struct {
	starts []adminui.CrawlStart
}

func (d *recordingDispatch) Start(
	_ context.Context,
	start adminui.CrawlStart,
) (adminui.CrawlDispatch, error) {
	d.starts = append(d.starts, start)

	return adminui.CrawlDispatch{ProfileHandle: "h", Seeds: len(start.Seeds)}, nil
}

func scheduleFixture(t *testing.T) *crawlschedule.Store {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	store, err := crawlschedule.Open(v, time.Now)
	if err != nil {
		t.Fatalf("crawlschedule.Open: %v", err)
	}

	return store
}

// TestCrawlScheduleSourceRoundTrip pins the UI-19 adapter: create validates
// through the store, the listing renders views, toggling and deleting work,
// and a malformed interval is rejected before the store sees it.
func TestCrawlScheduleSourceRoundTrip(t *testing.T) {
	store := scheduleFixture(t)
	source := newCrawlScheduleSource(store, &recordingDispatch{})
	ctx := context.Background()

	if err := source.CreateSchedule(ctx, adminui.CrawlScheduleRequest{
		Name: "Docs", Seeds: []string{"https://docs.example"},
		Scope: "domain", MaxDepth: 2, Interval: "24h",
	}); err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if err := source.CreateSchedule(ctx, adminui.CrawlScheduleRequest{
		Name: "Bad", Seeds: []string{"https://x.example"}, Interval: "soon",
	}); err == nil {
		t.Fatal("malformed interval must be rejected")
	}

	views, err := source.Schedules(ctx)
	if err != nil {
		t.Fatalf("Schedules: %v", err)
	}
	if len(views) != 1 || views[0].Name != "Docs" || views[0].Seeds != 1 ||
		views[0].LastRun != "never" || !views[0].Enabled {
		t.Fatalf("views = %+v", views)
	}

	if err := source.SetScheduleEnabled(ctx, views[0].ID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := source.Schedules(ctx)
	if err != nil {
		t.Fatalf("Schedules after disable: %v", err)
	}
	if got[0].Enabled {
		t.Fatal("disable did not stick")
	}
	if err := source.DeleteSchedule(ctx, views[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err = source.Schedules(ctx)
	if err != nil {
		t.Fatalf("Schedules after delete: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("delete left %+v", got)
	}

	if newCrawlScheduleSource(nil, &recordingDispatch{}) != nil ||
		newCrawlScheduleSource(store, nil) != nil {
		t.Fatal("missing parts must disable the source")
	}
}

// TestDispatchDueSchedules pins the loop body: a due schedule dispatches once
// through the console path and is deferred a full interval, so an immediate
// second pass dispatches nothing.
func TestDispatchDueSchedules(t *testing.T) {
	store := scheduleFixture(t)
	dispatch := &recordingDispatch{}
	ctx := context.Background()
	if _, err := store.Create(ctx, crawlschedule.Schedule{
		Name: "Docs", Seeds: []string{"https://docs.example"},
		Scope: "domain", MaxDepth: 2, Interval: 24 * time.Hour,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dispatchDueSchedules(ctx, store, dispatch)
	if len(dispatch.starts) != 1 || dispatch.starts[0].Name != "Docs" ||
		dispatch.starts[0].Scope != "domain" {
		t.Fatalf("starts = %+v", dispatch.starts)
	}

	dispatchDueSchedules(ctx, store, dispatch)
	if len(dispatch.starts) != 1 {
		t.Fatalf("just-ran schedule dispatched again: %d", len(dispatch.starts))
	}
}
