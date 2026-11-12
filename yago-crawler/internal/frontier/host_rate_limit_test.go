package frontier

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestHostRateLimitEscalatesAndUsesLongerRetryTime(t *testing.T) {
	frontier := openFrontier(t, 10)
	now := time.Unix(1_800_000_000, 0).UTC()
	first, err := frontier.RecordHostRateLimit(
		context.Background(), "Example.test", now, now,
	)
	if err != nil || !first.Equal(now.Add(time.Second)) {
		t.Fatalf("first host cooldown = %s, err %v", first, err)
	}
	secondRetry := now.Add(30 * time.Second)
	second, err := frontier.RecordHostRateLimit(
		context.Background(), "example.test", now.Add(time.Second), secondRetry,
	)
	if err != nil || !second.Equal(secondRetry) {
		t.Fatalf("server-directed host cooldown = %s, err %v", second, err)
	}
	third, err := frontier.RecordHostRateLimit(
		context.Background(), "example.test", now.Add(2*time.Second), now.Add(3*time.Second),
	)
	if err != nil || !third.Equal(secondRetry) {
		t.Fatalf("existing host cooldown was shortened: %s, err %v", third, err)
	}
	if hostRateLimitBackoff(2, time.Second) != 2*time.Second {
		t.Fatal("second consecutive rate limit did not double the cooldown")
	}
}

func TestHostRateLimitRejectsMissingHostAndRetryTime(t *testing.T) {
	frontier := openFrontier(t, 10)
	now := time.Unix(1_800_000_000, 0).UTC()
	for _, testCase := range []struct {
		name     string
		hostname string
		retryAt  time.Time
	}{
		{name: "missing host", retryAt: now.Add(time.Second)},
		{name: "missing retry time", hostname: "example.test"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := frontier.RecordHostRateLimit(
				context.Background(), testCase.hostname, now, testCase.retryAt,
			); err == nil {
				t.Fatal("invalid host rate limit was recorded")
			}
		})
	}
}

func TestHostRateLimitKeepsOtherHostsAvailableAcrossRuns(t *testing.T) {
	frontier, err := Open(filepath.Join(t.TempDir(), "frontier.db"), Config{
		MaximumPendingPages: 10,
		LeaseDuration:       time.Minute,
		HostDelay:           time.Second,
		MaximumHostActive:   1,
	})
	if err != nil {
		t.Fatalf("open frontier: %v", err)
	}
	t.Cleanup(func() { _ = frontier.Close() })
	now := time.Unix(1_800_000_000, 0).UTC()
	for _, testCase := range []struct{ runID, url string }{
		{"run-a", "https://one.example/active"},
		{"run-b", "https://one.example/waiting"},
		{"run-b", "https://two.example/available"},
	} {
		if _, _, err := frontier.Enqueue(
			context.Background(), testCase.runID, testCase.url, "", 0, now,
		); err != nil {
			t.Fatalf("enqueue %s: %v", testCase.url, err)
		}
	}
	activePage, found, err := frontier.Claim(context.Background(), "run-a", now)
	if err != nil || !found || activePage.Host != "one.example" {
		t.Fatalf("active host claim = %#v, found %v, err %v", activePage, found, err)
	}
	cooldownUntil, err := frontier.RecordHostRateLimit(
		context.Background(), activePage.Host, now, now.Add(20*time.Second),
	)
	if err != nil {
		t.Fatalf("record host rate limit: %v", err)
	}
	if err := frontier.Settle(context.Background(), activePage.ID, StateDeferred, cooldownUntil); err != nil {
		t.Fatalf("settle rate-limited page: %v", err)
	}
	availablePage, found, err := frontier.Claim(context.Background(), "run-b", now.Add(time.Second))
	if err != nil || !found || availablePage.Host != "two.example" {
		t.Fatalf("available host claim = %#v, found %v, err %v", availablePage, found, err)
	}
}

func TestSuccessfulSettlementResetsHostRateLimitStreak(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	if _, err := frontier.RecordHostRateLimit(ctx, "one.example", now, now.Add(time.Second)); err != nil {
		t.Fatalf("record host rate limit: %v", err)
	}
	page, _, err := frontier.Enqueue(ctx, "run-a", "https://one.example/page", "", 0, now)
	if err != nil {
		t.Fatalf("enqueue page: %v", err)
	}
	claimed, found, err := frontier.Claim(ctx, "run-a", now.Add(2*time.Second))
	if err != nil || !found || claimed.ID != page.ID {
		t.Fatalf("claim recovered host page = %#v, found %v, err %v", claimed, found, err)
	}
	if err := frontier.Settle(ctx, page.ID, StateFetched, time.Time{}); err != nil {
		t.Fatalf("settle successful page: %v", err)
	}
	var schedule hostSchedule
	if err := frontier.database.View(func(transaction *bolt.Tx) error {
		var readErr error
		schedule, readErr = loadHostSchedule(
			transaction.Bucket([]byte(hostsBucketName)).Get([]byte("one.example")),
		)
		return readErr
	}); err != nil {
		t.Fatalf("read host schedule: %v", err)
	}
	if schedule.RateLimitStreak != 0 {
		t.Fatalf("rate limit streak after success = %d", schedule.RateLimitStreak)
	}
}
