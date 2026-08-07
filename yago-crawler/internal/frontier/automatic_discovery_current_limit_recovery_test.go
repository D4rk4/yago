package frontier_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestAutomaticDiscoveryCheckpointRecoversUnderCurrentPageLimit(t *testing.T) {
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
	pageNames := make([]string, 30)
	for index := range pageNames {
		pageNames[index] = fmt.Sprintf("page-%02d", index)
	}
	provenance := []byte("current-automatic-budget")
	identity := []byte("current-automatic-budget-order")
	firstCheckpoint := openRestartCheckpoint(t, path, "old automatic limit")
	writeLegacyAutomaticDiscoveryCheckpoint(t, firstCheckpoint, legacyAutomaticCheckpointRun{
		provenance: provenance,
		identity:   identity,
		pages:      legacyAutomaticCheckpointPages(oldProfile.Profile.Handle, pageNames...),
	})
	closeRestartCheckpoint(t, firstCheckpoint, "old automatic limit")

	currentMaximum := 25
	currentDefinition := oldProfile.Profile
	currentDefinition.MaxDepth = 1
	currentDefinition.MaxPagesPerHost = 25
	currentDefinition.MaxPagesPerRun = &currentMaximum
	currentProfile, err := crawladmission.CompileProfile(currentDefinition)
	if err != nil {
		t.Fatalf("compile current automatic limit: %v", err)
	}
	if currentProfile.Profile.Handle != oldProfile.Profile.Handle {
		t.Fatalf(
			"current handle = %q, want stored %q",
			currentProfile.Profile.Handle,
			oldProfile.Profile.Handle,
		)
	}
	secondCheckpoint := openRestartCheckpoint(t, path, "current automatic limit")
	t.Cleanup(func() { _ = secondCheckpoint.Close() })
	crawlFrontier := frontier.NewFrontier(30, nil, frontier.WithCheckpoint(secondCheckpoint))
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
	if seeded.Queued != 25 {
		t.Fatalf("recovered pages = %d, want current limit 25", seeded.Queued)
	}
	snapshot, err := secondCheckpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load current automatic checkpoint: %v", err)
	}
	if snapshot.Counters.Pending != 25 || snapshot.BudgetDiscardedPages != 5 {
		t.Fatalf("current automatic checkpoint = %+v", snapshot)
	}
}
