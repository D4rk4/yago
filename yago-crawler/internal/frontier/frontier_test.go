package frontier

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openFrontier(t *testing.T, maximum uint64) *Frontier {
	t.Helper()
	frontier, err := Open(filepath.Join(t.TempDir(), "frontier.db"), Config{
		MaximumPendingPages: maximum,
		LeaseDuration:       time.Minute,
		HostDelay:           time.Second,
		MaximumHostActive:   1,
	})
	if err != nil {
		t.Fatalf("open frontier: %v", err)
	}
	t.Cleanup(func() { _ = frontier.Close() })
	return frontier
}

func TestEnqueueCanonicalizesAndDeduplicatesWithinRun(t *testing.T) {
	frontier := openFrontier(t, 10)
	first, added, err := frontier.Enqueue(
		context.Background(), "run-a", "https://Example.org/article?utm_source=feed", "", 0, time.Time{},
	)
	if err != nil || !added {
		t.Fatalf("first enqueue = added %v, err %v", added, err)
	}
	second, added, err := frontier.Enqueue(
		context.Background(), "run-a", "https://example.org/article", "", 0, time.Time{},
	)
	if err != nil || added || first.ID != second.ID || second.URL != "https://example.org/article" {
		t.Fatalf("duplicate enqueue = page %#v, added %v, err %v", second, added, err)
	}
	depth, err := frontier.Depth(context.Background())
	if err != nil || depth.Queued != 1 {
		t.Fatalf("depth = %#v, err %v", depth, err)
	}
}

func TestClaimDefersBusyHostAndContinuesWithAnotherHost(t *testing.T) {
	frontier := openFrontier(t, 10)
	now := time.Unix(1_800_000_000, 0).UTC()
	for _, value := range []struct{ runID, url string }{
		{"run-a", "https://one.example/first"},
		{"run-a", "https://one.example/second"},
		{"run-b", "https://two.example/first"},
	} {
		if _, _, err := frontier.Enqueue(context.Background(), value.runID, value.url, "", 0, now); err != nil {
			t.Fatalf("enqueue %s: %v", value.url, err)
		}
	}
	first, found, err := frontier.Claim(context.Background(), "run-a", now)
	if err != nil || !found || first.URL != "https://one.example/first" {
		t.Fatalf("first claim = %#v, found %v, err %v", first, found, err)
	}
	if err := frontier.Settle(context.Background(), first.ID, StateFetched, time.Time{}); err != nil {
		t.Fatalf("settle first host page: %v", err)
	}
	second, found, err := frontier.Claim(context.Background(), "run-b", now)
	if err != nil || !found || second.URL != "https://two.example/first" {
		t.Fatalf("second claim = %#v, found %v, err %v", second, found, err)
	}
	third, found, err := frontier.Claim(context.Background(), "run-a", now.Add(time.Second))
	if err != nil || !found || third.URL != "https://one.example/second" {
		t.Fatalf("third claim = %#v, found %v, err %v", third, found, err)
	}
}

func TestExpiredLeaseReturnsPageToReadyFrontier(t *testing.T) {
	frontier := openFrontier(t, 10)
	now := time.Unix(1_800_000_000, 0).UTC()
	page, _, err := frontier.Enqueue(
		context.Background(), "run-a", "https://one.example/first", "", 0, now,
	)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, found, err := frontier.Claim(context.Background(), "run-a", now); err != nil || !found {
		t.Fatalf("claim: found %v, err %v", found, err)
	}
	recovered, err := frontier.RecoverExpired(context.Background(), now.Add(time.Minute))
	if err != nil || recovered != 1 {
		t.Fatalf("recovery = %d, err %v", recovered, err)
	}
	depth, err := frontier.Depth(context.Background())
	if err != nil || depth.Queued != 1 || depth.Fetching != 0 {
		t.Fatalf("depth after recovery = %#v, err %v", depth, err)
	}
	claimed, found, err := frontier.Claim(context.Background(), "run-a", now.Add(time.Minute))
	if err != nil || !found || claimed.ID != page.ID || claimed.Attempts != 2 {
		t.Fatalf("reclaimed page = %#v, found %v, err %v", claimed, found, err)
	}
}

func TestFrontierRejectsPendingGrowthAndAcceptsCompletion(t *testing.T) {
	frontier := openFrontier(t, 1)
	now := time.Unix(1_800_000_000, 0).UTC()
	page, _, err := frontier.Enqueue(
		context.Background(), "run-a", "https://one.example/first", "", 0, now,
	)
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	if _, _, err := frontier.Enqueue(
		context.Background(), "run-a", "https://one.example/second", "", 0, now,
	); !errors.Is(err, ErrPendingLimit) {
		t.Fatalf("enqueue over limit error = %v", err)
	}
	claimed, found, err := frontier.Claim(context.Background(), "run-a", now)
	if err != nil || !found || claimed.ID != page.ID {
		t.Fatalf("claim = %#v, found %v, err %v", claimed, found, err)
	}
	if err := frontier.Settle(context.Background(), page.ID, StateFetched, time.Time{}); err != nil {
		t.Fatalf("settle fetched: %v", err)
	}
	if _, added, err := frontier.Enqueue(
		context.Background(), "run-b", "https://one.example/second", "", 0, now,
	); err != nil || !added {
		t.Fatalf("enqueue after completion = added %v, err %v", added, err)
	}
	depth, err := frontier.Depth(context.Background())
	if err != nil || depth.Fetched != 1 || depth.Queued != 1 {
		t.Fatalf("final depth = %#v, err %v", depth, err)
	}
}

func TestRunDepthAndClaimsStayWithinTheirCrawl(t *testing.T) {
	frontier := openFrontier(t, 10)
	now := time.Unix(1_800_000_000, 0).UTC()
	first, _, err := frontier.Enqueue(
		context.Background(), "run-a", "https://one.example/first", "", 0, now,
	)
	if err != nil {
		t.Fatalf("enqueue run-a page: %v", err)
	}
	second, _, err := frontier.Enqueue(
		context.Background(), "run-b", "https://two.example/first", "", 0, now,
	)
	if err != nil {
		t.Fatalf("enqueue run-b page: %v", err)
	}
	claimed, found, err := frontier.Claim(context.Background(), "run-b", now)
	if err != nil || !found || claimed.ID != second.ID {
		t.Fatalf("run-b claim = %#v, found %v, err %v", claimed, found, err)
	}
	if err := frontier.Settle(context.Background(), second.ID, StateIndexed, time.Time{}); err != nil {
		t.Fatalf("settle run-b page: %v", err)
	}
	firstDepth, err := frontier.DepthForRun(context.Background(), "run-a")
	if err != nil || firstDepth.Queued != 1 || firstDepth.Fetching != 0 || firstDepth.Fetched != 0 {
		t.Fatalf("run-a depth = %#v, err %v", firstDepth, err)
	}
	secondDepth, err := frontier.DepthForRun(context.Background(), "run-b")
	if err != nil || secondDepth.Queued != 0 || secondDepth.Fetching != 0 || secondDepth.Indexed != 1 {
		t.Fatalf("run-b depth = %#v, err %v", secondDepth, err)
	}
	claimed, found, err = frontier.Claim(context.Background(), "run-a", now)
	if err != nil || !found || claimed.ID != first.ID {
		t.Fatalf("run-a claim = %#v, found %v, err %v", claimed, found, err)
	}
}

func TestHostPageLimitAppliesWithinOneRunAndKeepsDuplicatesStable(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	first, added, err := frontier.EnqueueLimited(
		ctx, "run-a", "https://one.example/first", "", 0, now, 1,
	)
	if err != nil || !added {
		t.Fatalf("first limited enqueue = added %v, err %v", added, err)
	}
	duplicate, added, err := frontier.EnqueueLimited(
		ctx, "run-a", first.URL, "", 0, now, 1,
	)
	if err != nil || added || duplicate.ID != first.ID {
		t.Fatalf("duplicate limited enqueue = %#v, added %v, err %v", duplicate, added, err)
	}
	if _, _, err := frontier.EnqueueLimited(
		ctx, "run-a", "https://one.example/second", "", 0, now, 1,
	); !errors.Is(err, ErrHostPageLimit) {
		t.Fatalf("same-host over-limit error = %v", err)
	}
	if _, added, err := frontier.EnqueueLimited(
		ctx, "run-a", "https://two.example/first", "", 0, now, 1,
	); err != nil || !added {
		t.Fatalf("different-host enqueue = added %v, err %v", added, err)
	}
	if _, added, err := frontier.EnqueueLimited(
		ctx, "run-b", "https://one.example/first", "", 0, now, 1,
	); err != nil || !added {
		t.Fatalf("other-run same-host enqueue = added %v, err %v", added, err)
	}
}

func TestRunPageLimitIsAtomicAcrossConcurrentAdmissions(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	first, added, err := frontier.EnqueueRunLimited(
		ctx, "run-a", "https://one.example/first", "", 0, now, 0, 2,
	)
	if err != nil || !added {
		t.Fatalf("first run-limited enqueue = added %v, err %v", added, err)
	}
	duplicate, added, err := frontier.EnqueueRunLimited(
		ctx, "run-a", first.URL, "", 0, now, 0, 1,
	)
	if err != nil || added || duplicate.ID != first.ID {
		t.Fatalf("duplicate run-limited enqueue = %#v, added %v, err %v", duplicate, added, err)
	}
	if _, added, err := frontier.EnqueueRunLimited(
		ctx, "run-a", "https://two.example/second", "", 0, now, 0, 2,
	); err != nil || !added {
		t.Fatalf("second run-limited enqueue = added %v, err %v", added, err)
	}
	if _, _, err := frontier.EnqueueRunLimited(
		ctx, "run-a", "https://three.example/third", "", 0, now, 0, 2,
	); !errors.Is(err, ErrRunPageLimit) {
		t.Fatalf("over-limit run enqueue error = %v", err)
	}
	if _, added, err := frontier.EnqueueRunLimited(
		ctx, "run-b", "https://three.example/third", "", 0, now, 0, 1,
	); err != nil || !added {
		t.Fatalf("separate run admission = added %v, err %v", added, err)
	}
}

func TestCancelRunRemovesOnlyQueuedAndDeferredPages(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	active, _, err := frontier.Enqueue(ctx, "run-a", "https://one.example/active", "", 0, now)
	if err != nil {
		t.Fatalf("enqueue active page: %v", err)
	}
	queued, _, err := frontier.Enqueue(ctx, "run-a", "https://two.example/queued", "", 0, now)
	if err != nil {
		t.Fatalf("enqueue queued page: %v", err)
	}
	if _, found, err := frontier.Claim(ctx, "run-a", now); err != nil || !found {
		t.Fatalf("claim active page: found %v, err %v", found, err)
	}
	cancelled, err := frontier.CancelRun(ctx, "run-a")
	if err != nil || cancelled != 1 {
		t.Fatalf("cancelled pages = %d, err %v", cancelled, err)
	}
	activeDepth, err := frontier.DepthForRun(ctx, "run-a")
	if err != nil || activeDepth.Fetching != 1 || activeDepth.Cancelled != 1 || activeDepth.Pending() != 1 {
		t.Fatalf("run depth after cancellation = %#v, err %v", activeDepth, err)
	}
	page, found, err := frontier.Claim(ctx, "run-a", now.Add(time.Second))
	if err != nil || found || page.ID != "" {
		t.Fatalf("cancelled ready page claim = %#v, found %v, err %v", page, found, err)
	}
	if err := frontier.Settle(ctx, active.ID, StateFetched, time.Time{}); err != nil {
		t.Fatalf("settle active page: %v", err)
	}
	depth, err := frontier.Depth(ctx)
	if err != nil || depth.Queued != 0 || depth.Fetching != 0 || depth.Cancelled != 1 || depth.Fetched != 1 {
		t.Fatalf("global depth after settlement = %#v, err %v", depth, err)
	}
	if queued.ID == active.ID {
		t.Fatal("frontier returned one identity for distinct URLs")
	}
}
