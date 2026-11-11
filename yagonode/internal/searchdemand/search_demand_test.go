package searchdemand

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	store, err := Open(storage)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store
}

func TestDemandAggregatesShownClickedAndFocusedSignals(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	first := time.Unix(1_800_000_000, 0).UTC()
	last := first.Add(time.Minute)
	if err := store.RecordShown(ctx, "Cafes nearby", "https://example.test/place", first); err != nil {
		t.Fatalf("RecordShown: %v", err)
	}
	if err := store.RecordShown(ctx, "Cafes nearby", "https://example.test/place/", last); err != nil {
		t.Fatalf("RecordShown duplicate URL: %v", err)
	}
	if err := store.RecordClicked(ctx, "Cafes nearby", "https://example.test/place", last); err != nil {
		t.Fatalf("RecordClicked: %v", err)
	}
	if err := store.RecordFocused(ctx, "Local crawl", "https://example.test/section", last); err != nil {
		t.Fatalf("RecordFocused: %v", err)
	}
	page, err := store.ReadPage(ctx, "", 10)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(page.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(page.Records))
	}
	var shown Record
	var focused Record
	for _, record := range page.Records {
		if record.Topic == "Cafes nearby" {
			shown = record
		}
		if record.Topic == "Local crawl" {
			focused = record
		}
	}
	if shown.Shown != 2 || shown.Clicked != 1 || !shown.LastSeen.Equal(last) ||
		!shown.LastClicked.Equal(last) {
		t.Fatalf("search demand = %+v", shown)
	}
	if !focused.FocusedAt.Equal(last) || focused.Shown != 0 || focused.Clicked != 0 {
		t.Fatalf("focused demand = %+v", focused)
	}
}

func TestDemandPageCursorAndLimits(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	for _, topic := range []string{"topic-a", "topic-b", "topic-c"} {
		if err := store.RecordShown(ctx, topic, "https://example.test/", time.Unix(1_800_000_000, 0)); err != nil {
			t.Fatalf("RecordShown(%q): %v", topic, err)
		}
	}
	first, err := store.ReadPage(ctx, "", 1)
	if err != nil {
		t.Fatalf("ReadPage first: %v", err)
	}
	if len(first.Records) != 1 || first.NextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	second, err := store.ReadPage(ctx, first.NextCursor, 1)
	if err != nil {
		t.Fatalf("ReadPage second: %v", err)
	}
	if len(second.Records) != 1 || second.NextCursor == "" ||
		first.Records[0].Topic == second.Records[0].Topic {
		t.Fatalf("second page = %+v", second)
	}
	third, err := store.ReadPage(ctx, second.NextCursor, 1)
	if err != nil {
		t.Fatalf("ReadPage third: %v", err)
	}
	if len(third.Records) != 1 || third.NextCursor != "" {
		t.Fatalf("third page = %+v", third)
	}
	if _, err := store.ReadPage(ctx, "invalid", 1); err == nil {
		t.Fatal("invalid cursor was accepted")
	}
	if _, err := store.ReadPage(ctx, "", MaximumPageSize+1); err == nil {
		t.Fatal("oversized page was accepted")
	}
}

func TestDemandUpdatesCursorDeliversChangedRecordsAfterPriorPages(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	at := time.Unix(1_800_000_000, 0).UTC()
	if err := store.RecordShown(ctx, "topic-a", "https://example.test/a", at); err != nil {
		t.Fatalf("RecordShown topic-a: %v", err)
	}
	if err := store.RecordShown(ctx, "topic-b", "https://example.test/b", at); err != nil {
		t.Fatalf("RecordShown topic-b: %v", err)
	}
	first, err := store.ReadUpdates(ctx, "", 1)
	if err != nil {
		t.Fatalf("ReadUpdates first: %v", err)
	}
	if len(first.Records) != 1 || first.NextCursor == "" {
		t.Fatalf("first update page = %+v", first)
	}
	if err := store.RecordClicked(ctx, first.Records[0].Topic, first.Records[0].URL, at.Add(time.Minute)); err != nil {
		t.Fatalf("RecordClicked: %v", err)
	}
	second, err := store.ReadUpdates(ctx, first.NextCursor, MaximumPageSize)
	if err != nil {
		t.Fatalf("ReadUpdates after first page: %v", err)
	}
	if len(second.Records) != 2 || second.NextCursor == first.NextCursor {
		t.Fatalf("updates after first page = %+v", second)
	}
	foundClick := false
	for _, record := range second.Records {
		if record.Topic == first.Records[0].Topic && record.Clicked == 1 {
			foundClick = true
		}
	}
	if !foundClick {
		t.Fatalf("updated record missing after cursor advance: %+v", second.Records)
	}
	last, err := store.ReadUpdates(ctx, second.NextCursor, MaximumPageSize)
	if err != nil {
		t.Fatalf("ReadUpdates at end: %v", err)
	}
	if len(last.Records) != 0 || last.NextCursor != second.NextCursor {
		t.Fatalf("end update page = %+v", last)
	}
}

func TestDemandRejectsInvalidInputsAndKeepsValidInputUnchanged(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if err := store.RecordShown(ctx, "  ", "https://example.test/", time.Now()); err == nil {
		t.Fatal("empty topic was accepted")
	}
	if err := store.RecordShown(ctx, "topic", "file:///tmp/private", time.Now()); err == nil {
		t.Fatal("unsupported URL was accepted")
	}
	at := time.Unix(1_800_000_000, 0).UTC()
	if err := store.RecordShown(ctx, "  Topic   Name ", "https://example.test/a", at); err != nil {
		t.Fatalf("RecordShown: %v", err)
	}
	page, err := store.ReadPage(ctx, "", 10)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(page.Records) != 1 || page.Records[0].Topic != "Topic Name" ||
		page.Records[0].URL != "https://example.test/a" || page.Records[0].Shown != 1 {
		t.Fatalf("valid demand = %+v", page.Records)
	}
}

func TestFocusedDemandBatchIsAtomicAndDeduplicated(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	at := time.Unix(1_800_000_000, 0).UTC()
	if err := store.RecordFocusedBatch(ctx, "topic", []string{
		"https://example.test/a", "https://example.test/a#section",
	}, at); err != nil {
		t.Fatalf("RecordFocusedBatch: %v", err)
	}
	if err := store.RecordFocusedBatch(ctx, "topic", []string{
		"https://example.test/b", "file:///tmp/private",
	}, at.Add(time.Minute)); err == nil {
		t.Fatal("invalid focused batch was accepted")
	}
	page, err := store.ReadPage(ctx, "", 10)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(page.Records) != 1 || page.Records[0].URL != "https://example.test/a" ||
		!page.Records[0].FocusedAt.Equal(at) {
		t.Fatalf("focused demand records = %+v", page.Records)
	}
}
