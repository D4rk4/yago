package crawlruns

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestLegacyStorageVersionOneBucketsRemainFrozen(t *testing.T) {
	want := []vault.Name{terminalDeliveryBucket}
	got := legacyStorageVersionOneBuckets()
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("legacy storage version one buckets = %v, want %v", got, want)
	}
}

func TestLegacyStorageMigrationWrapsVaultFailure(t *testing.T) {
	err := MigrateLegacyStorage(t.Context(), nil, nil)
	if err == nil || !strings.Contains(
		err.Error(),
		"migrate legacy crawl run storage: invalid retained bucket migration",
	) {
		t.Fatalf("migration failure = %v", err)
	}
}

func TestLegacyStorageMigrationCopiesTerminalDeliveryIntoBolt(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "legacy.db")
	source, err := boltvault.OpenWithLockTimeout(sourcePath, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	initialSource := source
	t.Cleanup(func() { _ = initialSource.Close() })
	sourceRuns, err := Open(ctx, source, 4)
	if err != nil {
		t.Fatalf("open source runs: %v", err)
	}
	identity := make([]byte, 32)
	identity[0] = 1
	progress := yagocrawlcontract.CrawlRunProgress{
		RunID:         "migrated-run",
		WorkerID:      "worker",
		ProfileHandle: "profile",
		ProfileName:   "migration",
		State:         yagocrawlcontract.CrawlRunFinished,
		Tally:         yagocrawlcontract.CrawlRunTally{Fetched: 3, Indexed: 2},
	}
	if err := sourceRuns.RecordTerminal(ctx, identity, progress); err != nil {
		t.Fatalf("record source terminal delivery: %v", err)
	}

	path := filepath.Join(root, "crawlbroker.db")
	target, err := boltvault.OpenWithLockTimeout(path, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	if err := MigrateLegacyStorage(ctx, source, target); err != nil {
		t.Fatalf("migrate legacy run storage: %v", err)
	}
	if err := target.Close(); err != nil {
		t.Fatalf("close migrated target: %v", err)
	}
	target, err = boltvault.OpenWithLockTimeout(path, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("reopen migrated target: %v", err)
	}
	t.Cleanup(func() { _ = target.Close() })
	targetRuns, err := Open(ctx, target, 4)
	if err != nil {
		t.Fatalf("open target runs: %v", err)
	}
	runs := targetRuns.Recent()
	if len(runs) != 1 || runs[0].RunID != progress.RunID || runs[0].Tally != progress.Tally {
		t.Fatalf("migrated runs = %+v", runs)
	}

	if err := source.Close(); err != nil {
		t.Fatalf("close legacy source: %v", err)
	}
	reopenedSource, err := boltvault.OpenWithLockTimeout(sourcePath, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("reopen legacy source: %v", err)
	}
	t.Cleanup(func() { _ = reopenedSource.Close() })
	retainedRuns, err := Open(ctx, reopenedSource, 4)
	if err != nil {
		t.Fatalf("open retained legacy runs: %v", err)
	}
	runs = retainedRuns.Recent()
	if len(runs) != 1 || runs[0].RunID != progress.RunID || runs[0].Tally != progress.Tally {
		t.Fatalf("retained legacy runs = %+v", runs)
	}
}
