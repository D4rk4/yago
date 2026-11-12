package frontier

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestClaimExcludingHostsFindsDifferentHostBeyondActiveHostBacklog(t *testing.T) {
	frontier, err := Open(filepath.Join(t.TempDir(), "frontier.db"), Config{
		MaximumPendingPages: 5000,
		LeaseDuration:       time.Minute,
		HostDelay:           time.Second,
		MaximumHostActive:   1,
	})
	if err != nil {
		t.Fatalf("open frontier: %v", err)
	}
	t.Cleanup(func() { _ = frontier.Close() })
	now := time.Unix(1_800_000_000, 0).UTC()
	for index := 0; index < maximumReadyScan+1; index++ {
		url := fmt.Sprintf("https://one.example/page-%05d", index)
		if _, _, err := frontier.Enqueue(context.Background(), "run-a", url, "", 0, now); err != nil {
			t.Fatalf("enqueue active host page %d: %v", index, err)
		}
	}
	if _, _, err := frontier.Enqueue(
		context.Background(), "run-a", "https://two.example/first", "", 0, now,
	); err != nil {
		t.Fatalf("enqueue available host page: %v", err)
	}
	first, found, err := frontier.Claim(context.Background(), "run-a", now)
	if err != nil || !found || first.Host != "one.example" {
		t.Fatalf("first claim = %#v, found %v, err %v", first, found, err)
	}
	claimed, found, err := frontier.ClaimExcludingHosts(
		context.Background(),
		"run-a",
		now,
		map[string]struct{}{first.Host: {}},
	)
	if err != nil || !found || claimed.Host != "two.example" {
		t.Fatalf("filtered claim = %#v, found %v, err %v", claimed, found, err)
	}
	depth, err := frontier.DepthForRun(context.Background(), "run-a")
	if err != nil || depth.Deferred != 0 {
		t.Fatalf("active host pages were requeued: depth %#v, err %v", depth, err)
	}
}
