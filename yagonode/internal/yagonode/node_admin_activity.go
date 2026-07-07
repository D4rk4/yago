package yagonode

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/searchactivity"
)

// activityTopWords bounds the top-words table.
const activityTopWords = 20

// activitySource adapts the search-activity tracker to the admin console; a
// nil tracker (query logging off) renders the section's unavailable state.
type activitySource struct {
	tracker *searchactivity.Tracker
}

// newActivitySource returns the console source, or nil when nothing records.
func newActivitySource(tracker *searchactivity.Tracker) adminui.ActivitySource {
	if tracker == nil {
		return nil
	}

	return activitySource{tracker: tracker}
}

func (s activitySource) Activity(_ context.Context) adminui.ActivityView {
	entries, total, zero := s.tracker.Snapshot()
	view := adminui.ActivityView{
		Mode:        string(s.tracker.Mode()),
		Total:       total,
		ZeroResults: zero,
		Entries:     make([]adminui.ActivityEntry, 0, len(entries)),
	}
	for _, entry := range entries {
		view.Entries = append(view.Entries, adminui.ActivityEntry{
			Time:     entry.At.Format("15:04:05"),
			Query:    entry.Query,
			Length:   entry.QueryLength,
			Terms:    entry.Terms,
			Results:  entry.Results,
			Duration: entry.Duration.Round(time.Millisecond).String(),
			Source:   entry.Source,
		})
	}
	for _, word := range s.tracker.TopWords(activityTopWords) {
		view.TopWords = append(view.TopWords, adminui.ActivityWord{
			Word: word.Word, Count: word.Count,
		})
	}

	return view
}
