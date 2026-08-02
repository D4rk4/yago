package searchlocal

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestFirstSeenBoundsReachTheIndexRequest(t *testing.T) {
	index := &fakeIndex{}
	minFirstSeen := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	maxFirstSeen := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	if _, err := NewSearcher(index).Search(t.Context(), searchcore.Request{
		Query:        "needle",
		MinFirstSeen: minFirstSeen,
		MaxFirstSeen: maxFirstSeen,
	}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !index.got.MinFirstSeen.Equal(minFirstSeen) ||
		!index.got.MaxFirstSeen.Equal(maxFirstSeen) {
		t.Fatalf("index request = %#v", index.got)
	}
	// A first-seen window is not a publication window.
	if !index.got.MinDate.IsZero() || !index.got.MaxDate.IsZero() {
		t.Fatalf("index date bounds = %v %v", index.got.MinDate, index.got.MaxDate)
	}
}

func TestFirstSeenBoundsSelectRecentlyDiscoveredPages(t *testing.T) {
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	// Neither page carries a publication date; they differ only in when this
	// node first saw them.
	recent := documentstore.Document{
		NormalizedURL: "https://docs.example/download",
		Title:         "Needle download",
		ExtractedText: "Needle download page",
		Language:      "en",
		FirstSeenAt:   time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
	}
	old := documentstore.Document{
		NormalizedURL: "https://docs.example/archive",
		Title:         "Needle archive",
		ExtractedText: "Needle archive page",
		Language:      "en",
		FirstSeenAt:   time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	for _, document := range []documentstore.Document{recent, old} {
		if err := index.Index(t.Context(), document); err != nil {
			t.Fatalf("Index: %v", err)
		}
	}
	searcher := NewSearcher(index)

	bounded, err := searcher.Search(t.Context(), searchcore.Request{
		Query:        "needle",
		Terms:        []string{"needle"},
		Limit:        10,
		MinFirstSeen: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(bounded.Results) != 1 || bounded.Results[0].URL != recent.NormalizedURL {
		t.Fatalf("first-seen bounded results = %#v", bounded.Results)
	}
	unbounded, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "needle",
		Terms: []string{"needle"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(unbounded.Results) != 2 {
		t.Fatalf("unbounded results = %#v", unbounded.Results)
	}
}
