package frontier

import (
	"context"
	"testing"
	"time"
)

func TestWaitingSampleReportsNextURLAndHostWait(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	_, _, err := frontier.Enqueue(ctx, "run-a", "https://one.example/active", "", 0, now)
	if err != nil {
		t.Fatalf("enqueue active URL: %v", err)
	}
	queued, _, err := frontier.Enqueue(ctx, "run-a", "https://one.example/waiting", "", 0, now)
	if err != nil {
		t.Fatalf("enqueue waiting URL: %v", err)
	}
	if _, found, err := frontier.Claim(ctx, "run-a", now); err != nil || !found {
		t.Fatalf("claim first URL: found %t, err %v", found, err)
	}

	sample, err := frontier.WaitingSample(ctx, []string{"run-a"}, 1, 1, now)
	if err != nil || len(sample) != 1 {
		t.Fatalf("waiting sample = %#v, err %v", sample, err)
	}
	if sample[0].Page.ID != queued.ID || sample[0].Page.URL != queued.URL ||
		sample[0].Reason != "host concurrency" || !sample[0].AvailableAt.IsZero() {
		t.Fatalf("waiting sample = %#v", sample[0])
	}
	depth, err := frontier.DepthForRun(ctx, "run-a")
	if err != nil || depth.Fetching != 1 || depth.Deferred != 0 {
		t.Fatalf("frontier changed while sampling = %#v, err %v", depth, err)
	}
}

func TestWaitingSampleReportsDeferredReadyTimeAndEmptyRuns(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	page, _, err := frontier.Enqueue(ctx, "run-a", "https://one.example/retry", "", 0, now)
	if err != nil {
		t.Fatalf("enqueue retry URL: %v", err)
	}
	if _, found, err := frontier.Claim(ctx, "run-a", now); err != nil || !found {
		t.Fatalf("claim retry URL: found %t, err %v", found, err)
	}
	retryAt := now.Add(3 * time.Second)
	if err := frontier.Settle(ctx, page.ID, StateDeferred, retryAt); err != nil {
		t.Fatalf("defer URL: %v", err)
	}

	sample, err := frontier.WaitingSample(ctx, []string{"run-a", "run-empty"}, 1, 2, now)
	if err != nil || len(sample) != 1 || sample[0].Page.State != StateDeferred ||
		sample[0].Reason != "retry delay" || !sample[0].AvailableAt.Equal(retryAt) {
		t.Fatalf("deferred waiting sample = %#v, err %v", sample, err)
	}
	empty, err := frontier.WaitingSample(ctx, []string{"run-empty"}, 1, 1, now)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty run sample = %#v, err %v", empty, err)
	}
}

func TestWaitingSampleRejectsInvalidRequests(t *testing.T) {
	frontier := openFrontier(t, 10)
	if _, err := frontier.WaitingSample(context.Background(), nil, 0, 1, time.Now()); err == nil {
		t.Fatal("accepted a nonpositive waiting sample limit")
	}
	if _, err := frontier.WaitingSample(context.Background(), []string{""}, 1, 1, time.Now()); err == nil {
		t.Fatal("accepted an empty crawl run identity")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := frontier.WaitingSample(ctx, []string{"run-a"}, 1, 1, time.Now()); err == nil {
		t.Fatal("accepted a cancelled sample request")
	}
}

func TestWaitingSampleBoundsPagesPerRunAndAcrossRuns(t *testing.T) {
	frontier := openFrontier(t, 10)
	now := time.Unix(1_800_000_000, 0).UTC()
	for _, url := range []string{
		"https://one.example/first",
		"https://two.example/second",
		"https://three.example/third",
	} {
		if _, _, err := frontier.Enqueue(context.Background(), "run-a", url, "", 0, now); err != nil {
			t.Fatalf("enqueue %s: %v", url, err)
		}
	}
	if _, _, err := frontier.Enqueue(
		context.Background(), "run-b", "https://four.example/fourth", "", 0, now,
	); err != nil {
		t.Fatalf("enqueue run-b URL: %v", err)
	}

	sample, err := frontier.WaitingSample(context.Background(), []string{"run-a", "run-b"}, 3, 2, now)
	if err != nil || len(sample) != 2 || sample[0].Page.RunID != "run-a" ||
		sample[1].Page.RunID != "run-a" {
		t.Fatalf("bounded waiting sample = %#v, err %v", sample, err)
	}
	all, err := frontier.WaitingSample(context.Background(), []string{"run-a", "run-b"}, 2, 4, now)
	if err != nil || len(all) != 3 || all[2].Page.RunID != "run-b" {
		t.Fatalf("per-run waiting sample = %#v, err %v", all, err)
	}
}
