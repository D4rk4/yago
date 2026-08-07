package frontier_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestAutomaticDiscoveryCheckpointRecoversUnderCurrentDepthLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	oldMaximum := 250
	oldProfile := compiled(t, yagocrawlcontract.CrawlProfile{
		Name:            "web-fallback-seed",
		Scope:           yagocrawlcontract.ScopeWide,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        5,
		MaxPagesPerHost: 250,
		MaxPagesPerRun:  &oldMaximum,
	})
	pages := legacyAutomaticCheckpointPages(
		oldProfile.Profile.Handle,
		"depth-zero",
		"depth-one",
		"depth-two",
		"depth-five",
	)
	for index, depth := range []int{0, 1, 2, 5} {
		pages[index].Depth = depth
	}
	provenance := []byte("current-automatic-depth")
	identity := []byte("current-automatic-depth-order")
	firstCheckpoint := openRestartCheckpoint(t, path, "old automatic depth")
	writeLegacyAutomaticDiscoveryCheckpoint(t, firstCheckpoint, legacyAutomaticCheckpointRun{
		provenance: provenance,
		identity:   identity,
		pages:      pages,
	})
	closeRestartCheckpoint(t, firstCheckpoint, "old automatic depth")

	currentDefinition := oldProfile.Profile
	currentDefinition.MaxDepth = 1
	currentProfile, err := crawladmission.CompileProfile(currentDefinition)
	if err != nil {
		t.Fatalf("compile current automatic depth: %v", err)
	}
	secondCheckpoint := openRestartCheckpoint(t, path, "current automatic depth")
	t.Cleanup(func() { _ = secondCheckpoint.Close() })
	crawlFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(secondCheckpoint))
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Provenance:    provenance,
			OrderIdentity: identity,
			Priority:      yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		},
		currentProfile,
		nil,
	)
	if seeded.Queued != 2 {
		t.Fatalf("recovered depth pages = %d, want 2", seeded.Queued)
	}
	snapshot, err := secondCheckpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load current automatic depth checkpoint: %v", err)
	}
	if snapshot.Counters.Pending != 2 || snapshot.BudgetDiscardedPages != 2 ||
		len(snapshot.Outstanding) != 2 || snapshot.Outstanding[0].Depth != 0 ||
		snapshot.Outstanding[1].Depth != 1 {
		t.Fatalf("current automatic depth checkpoint = %+v", snapshot)
	}
}
