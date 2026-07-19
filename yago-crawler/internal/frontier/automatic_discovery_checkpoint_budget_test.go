package frontier_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestLegacyAutomaticDiscoveryCheckpointDropsPendingPagesAboveTaskLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeWide,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: 2,
	})
	provenance := []byte("legacy-automatic-budget")
	identity := []byte("legacy-automatic-budget-order")
	firstCheckpoint := openRestartCheckpoint(t, path, "legacy automatic")
	pages := legacyAutomaticCheckpointPages(
		profile.Profile.Handle,
		"one",
		"two",
		"three",
		"four",
	)
	run := legacyAutomaticCheckpointRun{
		provenance: provenance, identity: identity, pages: pages,
	}
	writeLegacyAutomaticDiscoveryCheckpoint(t, firstCheckpoint, run)
	closeRestartCheckpoint(t, firstCheckpoint, "legacy automatic")

	secondCheckpoint := openRestartCheckpoint(t, path, "bounded automatic")
	crawlFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(secondCheckpoint))
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Provenance:    provenance,
			OrderIdentity: identity,
			Priority:      yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		},
		profile,
		nil,
	)
	if seeded.Queued != 2 {
		t.Fatalf("recovered queued = %d, want 2", seeded.Queued)
	}
	snapshot, err := secondCheckpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load bounded automatic checkpoint: %v", err)
	}
	if snapshot.Counters.Pending != 2 || len(snapshot.Outstanding) != 2 ||
		snapshot.BudgetDiscardedPages != 2 ||
		snapshot.Outstanding[0].URL != pages[0].URL || snapshot.Outstanding[1].URL != pages[1].URL {
		t.Fatalf("bounded automatic snapshot = %+v", snapshot)
	}
	closeRestartCheckpoint(t, secondCheckpoint, "trimmed automatic")

	thirdCheckpoint := openRestartCheckpoint(t, path, "restarted bounded automatic")
	t.Cleanup(func() { _ = thirdCheckpoint.Close() })
	restartedFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(thirdCheckpoint))
	finished := make(chan bool, 1)
	restarted := restartedFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Provenance:    provenance,
			OrderIdentity: identity,
			Priority:      yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	if restarted.Queued != 2 {
		t.Fatalf("restarted queued = %d, want 2", restarted.Queued)
	}
	restartedSnapshot, err := thirdCheckpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load restarted automatic checkpoint: %v", err)
	}
	if restartedSnapshot.Counters.Pending != 2 ||
		restartedSnapshot.BudgetDiscardedPages != 2 ||
		restartedSnapshot.Outstanding[0].URL != pages[0].URL ||
		restartedSnapshot.Outstanding[1].URL != pages[1].URL {
		t.Fatalf("restarted automatic snapshot = %+v", restartedSnapshot)
	}
	for range 2 {
		restartedFrontier.Done(receiveJob(t, restartedFrontier), successfulPageOutcome())
	}
	expectSuccessfulSettlement(t, finished, "bounded legacy automatic run")
	assertNoJob(t, restartedFrontier, 50*time.Millisecond)
}

func TestLegacyAutomaticDiscoveryCheckpointStopsAfterCompletedTaskLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeWide,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: 2,
	})
	provenance := []byte("legacy-completed-budget")
	identity := []byte("legacy-completed-budget-order")
	firstCheckpoint := openRestartCheckpoint(t, path, "legacy completed automatic")
	pages := legacyAutomaticCheckpointPages(
		profile.Profile.Handle,
		"one",
		"two",
		"three",
		"four",
		"five",
	)
	run := legacyAutomaticCheckpointRun{
		provenance: provenance, identity: identity, pages: pages, completedPages: 3,
	}
	writeLegacyAutomaticDiscoveryCheckpoint(t, firstCheckpoint, run)
	closeRestartCheckpoint(t, firstCheckpoint, "legacy completed automatic")

	secondCheckpoint := openRestartCheckpoint(t, path, "stopped completed automatic")
	t.Cleanup(func() { _ = secondCheckpoint.Close() })
	crawlFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(secondCheckpoint))
	finished := make(chan bool, 1)
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Provenance:    provenance,
			OrderIdentity: identity,
			Priority:      yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	if seeded.Queued != 0 {
		t.Fatalf("recovered queued = %d, want 0", seeded.Queued)
	}
	expectSuccessfulSettlement(t, finished, "completed legacy automatic run")
	snapshot, err := secondCheckpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load stopped automatic checkpoint: %v", err)
	}
	if snapshot.Counters.Pending != 0 || len(snapshot.Outstanding) != 0 || !snapshot.Completed {
		t.Fatalf("stopped automatic snapshot = %+v", snapshot)
	}
	assertNoJob(t, crawlFrontier, 50*time.Millisecond)
}
