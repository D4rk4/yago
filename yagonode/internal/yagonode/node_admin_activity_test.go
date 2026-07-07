package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchactivity"
)

// TestActivitySourceRendersTrackerState pins the UI-16 adapter: entries map
// onto the view newest-first with formatted time and duration, top words
// appear in full mode, and a nil tracker hides the section.
func TestActivitySourceRendersTrackerState(t *testing.T) {
	if newActivitySource(nil) != nil {
		t.Fatal("nil tracker must yield a nil source (section unavailable)")
	}

	tracker := searchactivity.New(searchactivity.ModeFull)
	tracker.Record(searchactivity.Entry{
		At: time.Date(2026, 7, 7, 10, 30, 45, 0, time.UTC), Query: "осень ддт",
		QueryLength: 9, Terms: 2, Results: 4,
		Duration: 1512 * time.Millisecond, Source: "global",
	})
	tracker.Record(searchactivity.Entry{
		At: time.Date(2026, 7, 7, 10, 31, 0, 0, time.UTC), Query: "осень",
		QueryLength: 5, Terms: 1, Results: 0,
		Duration: 90 * time.Millisecond, Source: "local",
	})

	view := newActivitySource(tracker).Activity(context.Background())
	if view.Mode != "full" || view.Total != 2 || view.ZeroResults != 1 {
		t.Fatalf("view header = %+v", view)
	}
	if len(view.Entries) != 2 || view.Entries[0].Query != "осень" ||
		view.Entries[0].Time != "10:31:00" || view.Entries[1].Duration != "1.512s" {
		t.Fatalf("entries = %+v", view.Entries)
	}
	if len(view.TopWords) == 0 || view.TopWords[0].Word != "осень" ||
		view.TopWords[0].Count != 2 {
		t.Fatalf("top words = %+v", view.TopWords)
	}
}
