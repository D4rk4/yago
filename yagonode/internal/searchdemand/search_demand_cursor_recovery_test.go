package searchdemand

import (
	"context"
	"testing"
	"time"
)

func TestReadUpdatesRestartsWhenCrawlerCursorIsAhead(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	at := time.Unix(1_800_000_000, 0).UTC()
	if err := store.RecordFocused(ctx, "topic", "https://example.test/", at); err != nil {
		t.Fatalf("RecordFocused: %v", err)
	}
	page, err := store.ReadUpdates(ctx, "55", MaximumPageSize)
	if err != nil {
		t.Fatalf("ReadUpdates with stale cursor: %v", err)
	}
	if len(page.Records) != 1 || page.NextCursor != "1" ||
		page.Records[0].URL != "https://example.test/" || !page.Records[0].FocusedAt.Equal(at) {
		t.Fatalf("recovered demand page = %+v", page)
	}
	current, err := store.ReadUpdates(ctx, page.NextCursor, MaximumPageSize)
	if err != nil {
		t.Fatalf("ReadUpdates at current cursor: %v", err)
	}
	if len(current.Records) != 0 || current.NextCursor != page.NextCursor {
		t.Fatalf("current demand page = %+v", current)
	}
}
