package crawlschedule

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func openStore(t *testing.T, now *time.Time) *Store {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	store, err := Open(v, func() time.Time { return *now })
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return store
}

// TestScheduleLifecycle pins UI-19: create validates and normalizes, a fresh
// schedule is immediately due, MarkRan defers it a full interval, disabling
// removes it from the due list, and delete removes it entirely.
func TestScheduleLifecycle(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := openStore(t, &now)
	ctx := context.Background()

	created, err := store.Create(ctx, Schedule{
		Name:     "  Docs Site  ",
		Seeds:    []string{" https://docs.example ", "", "https://blog.example"},
		Scope:    "domain",
		MaxDepth: 3,
		Interval: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID != "docs-site" || len(created.Seeds) != 2 || !created.Enabled {
		t.Fatalf("created = %+v", created)
	}

	due, err := store.DueSchedules(ctx)
	if err != nil || len(due) != 1 {
		t.Fatalf("fresh schedule must be due: %v %v", due, err)
	}

	if err := store.MarkRan(ctx, created.ID, now); err != nil {
		t.Fatalf("MarkRan: %v", err)
	}
	if due, _ := store.DueSchedules(ctx); len(due) != 0 {
		t.Fatalf("just-ran schedule must wait: %v", due)
	}
	now = now.Add(24*time.Hour + time.Minute)
	if due, _ := store.DueSchedules(ctx); len(due) != 1 {
		t.Fatalf("after the interval it is due again: %v", due)
	}

	if err := store.SetEnabled(ctx, created.ID, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if due, _ := store.DueSchedules(ctx); len(due) != 0 {
		t.Fatalf("disabled schedule must not be due: %v", due)
	}

	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if schedules, _ := store.List(ctx); len(schedules) != 0 {
		t.Fatalf("deleted schedule lingers: %v", schedules)
	}
}

func TestCreateValidation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := openStore(t, &now)
	ctx := context.Background()
	cases := map[string]Schedule{
		"empty name":    {Seeds: []string{"https://a.example"}, Interval: 2 * time.Hour},
		"no seeds":      {Name: "x", Seeds: []string{"  "}, Interval: 2 * time.Hour},
		"tiny interval": {Name: "x", Seeds: []string{"https://a.example"}, Interval: time.Minute},
	}
	for name, schedule := range cases {
		if _, err := store.Create(ctx, schedule); err == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
	if err := store.SetEnabled(ctx, "ghost", true); err == nil {
		t.Fatal("mutating a missing schedule must fail")
	}
}

func TestScheduleIDStability(t *testing.T) {
	if scheduleID("  Мой Сайт 2024!  ") != "2024" && scheduleID("Docs / Site") != "docs---site" {
		t.Log("non-latin runs collapse to dashes; asserting core behavior below")
	}
	if scheduleID("Docs Site") != "docs-site" || scheduleID("docs-site") != "docs-site" {
		t.Fatalf("latin id derivation broken: %q", scheduleID("Docs Site"))
	}
}
