package frontier

import (
	"context"
	"testing"
	"time"
)

func TestPendingIngestSurvivesFetchSettlementAndCompletesIndexing(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	page, added, err := frontier.Enqueue(
		ctx, "run-a", "https://one.example/page", "", 0, time.Time{},
	)
	if err != nil || !added {
		t.Fatalf("enqueue = added %t, err %v", added, err)
	}
	if _, found, err := frontier.Claim(ctx, page.RunID, time.Now()); err != nil || !found {
		t.Fatalf("claim = found %t, err %v", found, err)
	}
	if err := frontier.StorePendingIngest(
		ctx,
		page.ID,
		"lease-a",
		[]byte(`{"batch":true}`),
	); err != nil {
		t.Fatalf("store pending ingest: %v", err)
	}
	if err := frontier.Settle(ctx, page.ID, StateFetched, time.Time{}); err != nil {
		t.Fatalf("settle fetched page: %v", err)
	}
	pending, found, err := frontier.NextPendingIngest(ctx, time.Now().Add(time.Second))
	if err != nil || !found || pending.PageID != page.ID || pending.LeaseID != "lease-a" ||
		pending.URL != page.URL || string(pending.BatchJSON) != `{"batch":true}` {
		t.Fatalf("pending ingest = %#v, found %t, err %v", pending, found, err)
	}
	if err := frontier.CompletePendingIngest(ctx, page.ID, true); err != nil {
		t.Fatalf("complete pending ingest: %v", err)
	}
	depth, err := frontier.DepthForRun(ctx, page.RunID)
	if err != nil || depth.Fetched != 0 || depth.Indexed != 1 {
		t.Fatalf("run depth after ingest = %#v, err %v", depth, err)
	}
	if pendingCount, err := frontier.PendingIngestForRun(
		ctx,
		page.RunID,
	); err != nil ||
		pendingCount != 0 {
		t.Fatalf("pending ingest count = %d, err %v", pendingCount, err)
	}
}

func TestPendingIngestRequiresFetchingPageAndCanRebindLease(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	page, _, err := frontier.Enqueue(
		ctx, "run-b", "https://one.example/page", "", 0, time.Time{},
	)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	batch := []byte(`{"batch":true}`)
	if err := frontier.StorePendingIngest(ctx, page.ID, "lease-a", batch); err == nil {
		t.Fatal("stored ingest for an unclaimed page")
	}
	if _, found, err := frontier.Claim(ctx, page.RunID, time.Now()); err != nil || !found {
		t.Fatalf("claim = found %t, err %v", found, err)
	}
	if err := frontier.StorePendingIngest(ctx, page.ID, "lease-a", batch); err != nil {
		t.Fatalf("store pending ingest: %v", err)
	}
	if err := frontier.Settle(ctx, page.ID, StateFetched, time.Time{}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if err := frontier.RebindPendingIngestLease(ctx, page.RunID, "lease-b"); err != nil {
		t.Fatalf("rebind lease: %v", err)
	}
	pending, found, err := frontier.NextPendingIngest(ctx, time.Now().Add(time.Second))
	if err != nil || !found || pending.LeaseID != "lease-b" {
		t.Fatalf("rebound pending ingest = %#v, found %t, err %v", pending, found, err)
	}
	if err := frontier.CompletePendingIngest(ctx, page.ID, false); err != nil {
		t.Fatalf("complete rejected ingest: %v", err)
	}
	depth, err := frontier.DepthForRun(ctx, page.RunID)
	if err != nil || depth.Fetched != 1 || depth.Indexed != 0 {
		t.Fatalf("rejected ingest changed page state = %#v, err %v", depth, err)
	}
}

func TestPendingIngestSelectionSkipsItemsClaimedByOtherSenders(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	for _, pageURL := range []string{
		"https://one.example/first",
		"https://two.example/second",
	} {
		page, _, err := frontier.Enqueue(ctx, "run-d", pageURL, "", 0, time.Time{})
		if err != nil {
			t.Fatalf("enqueue %s: %v", pageURL, err)
		}
		if _, found, err := frontier.Claim(ctx, page.RunID, time.Now()); err != nil || !found {
			t.Fatalf("claim %s = found %t, err %v", pageURL, found, err)
		}
		if err := frontier.StorePendingIngest(
			ctx,
			page.ID,
			"lease-d",
			[]byte(`{"batch":true}`),
		); err != nil {
			t.Fatalf("store pending ingest for %s: %v", pageURL, err)
		}
		if err := frontier.Settle(ctx, page.ID, StateFetched, time.Time{}); err != nil {
			t.Fatalf("settle %s: %v", pageURL, err)
		}
	}
	first, found, err := frontier.NextPendingIngest(ctx, time.Now().Add(time.Second))
	if err != nil || !found {
		t.Fatalf("first sender selection = found %t, err %v", found, err)
	}
	second, found, err := frontier.NextPendingIngestSkipping(
		ctx,
		time.Now().Add(time.Second),
		map[string]struct{}{first.PageID: {}},
	)
	if err != nil || !found || second.PageID == first.PageID {
		t.Fatalf("second sender selection = %#v, found %t, err %v", second, found, err)
	}
}

func TestPendingIngestRejectsInvalidBatchSizeAndRetryWaitsUntilDue(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	page, _, err := frontier.Enqueue(
		ctx, "run-c", "https://one.example/page", "", 0, time.Time{},
	)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := frontier.StorePendingIngest(ctx, page.ID, "lease-a", []byte("{")); err == nil {
		t.Fatal("stored invalid ingest JSON")
	}
	if _, found, err := frontier.Claim(ctx, page.RunID, time.Now()); err != nil || !found {
		t.Fatalf("claim = found %t, err %v", found, err)
	}
	if err := frontier.StorePendingIngest(
		ctx,
		page.ID,
		"lease-a",
		[]byte(`{"batch":true}`),
	); err != nil {
		t.Fatalf("store pending ingest: %v", err)
	}
	if err := frontier.Settle(ctx, page.ID, StateFetched, time.Time{}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	due := time.Now().Add(time.Minute)
	if err := frontier.RetryPendingIngest(ctx, page.ID, due, true); err != nil {
		t.Fatalf("schedule retry: %v", err)
	}
	if _, found, err := frontier.NextPendingIngest(ctx, time.Now()); err != nil || found {
		t.Fatalf("early retry = found %t, err %v", found, err)
	}
	if err := frontier.RebindPendingIngestLease(ctx, page.RunID, "lease-b"); err != nil {
		t.Fatalf("rebind: %v", err)
	}
	if _, found, err := frontier.NextPendingIngest(ctx, time.Now()); err != nil || !found {
		t.Fatalf("rebound retry = found %t, err %v", found, err)
	}
	if err := frontier.CompletePendingIngest(ctx, page.ID, false); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := frontier.RetryPendingIngest(ctx, page.ID, time.Now(), false); err != nil {
		t.Fatalf("idempotent retry after completion: %v", err)
	}
}
