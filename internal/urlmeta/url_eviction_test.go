package urlmeta_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func openVaultAndModule(t *testing.T) (*boltvault.Vault, urlmeta.Module) {
	t.Helper()

	vault, err := boltvault.Open(filepath.Join(t.TempDir(), "node.db"), 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := vault.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	guard := httpguard.NewRequestGuard(localIdentity(), httpguard.DefaultMaxBodyBytes, time.Second)
	module, err := urlmeta.New(vault, guard, fixedStatus{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return vault, module
}

func urlRowWithFreshness(t *testing.T, seed, freshness string) yacymodel.URIMetadataRow {
	t.Helper()

	row := yacymodel.URIMetadataRow{
		Properties: map[string]string{
			yacymodel.URLMetaHash: yacymodel.WordHash(seed).String(),
			yacymodel.ColLoadDate: freshness,
		},
	}
	roundTrip, err := yacymodel.ParseURIMetadataRow(row.String())
	if err != nil {
		t.Fatalf("row does not round-trip: %v", err)
	}

	return roundTrip
}

func TestSelectStaleReturnsStalestFirst(t *testing.T) {
	ctx := context.Background()
	module := openModule(t, 0)

	rows := []yacymodel.URIMetadataRow{
		urlRowWithFreshness(t, "fresh", "20260101"),
		urlRowWithFreshness(t, "stale", "20200101"),
		urlRowWithFreshness(t, "middle", "20230101"),
	}
	if _, err := module.Intake(ctx, rows); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	candidates, err := module.Evictor.SelectStale(ctx, 2)
	if err != nil {
		t.Fatalf("SelectStale: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(candidates))
	}
	if candidates[0] != rowHash(t, rows[1]) {
		t.Fatalf("candidates[0] = %v, want stale", candidates[0])
	}
	if candidates[1] != rowHash(t, rows[2]) {
		t.Fatalf("candidates[1] = %v, want middle", candidates[1])
	}
}

func TestSelectStaleZeroLimit(t *testing.T) {
	candidates, err := openModule(t, 0).Evictor.SelectStale(context.Background(), 0)
	if err != nil {
		t.Fatalf("SelectStale: %v", err)
	}
	if candidates != nil {
		t.Fatalf("candidates = %v, want nil", candidates)
	}
}

func TestPurgeDeletesRows(t *testing.T) {
	ctx := context.Background()
	vault, module := openVaultAndModule(t)
	row := urlRow(t, "a")
	if _, err := module.Intake(ctx, []yacymodel.URIMetadataRow{row, urlRow(t, "b")}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	var result urlmeta.PurgeResult
	if err := vault.Update(ctx, func(tx *boltvault.Txn) error {
		purged, purgeErr := module.Evictor.Purge(tx, []yacymodel.Hash{rowHash(t, row)})
		result = purged
		if purgeErr != nil {
			return fmt.Errorf("purge: %w", purgeErr)
		}

		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if result.URLsDeleted != 1 {
		t.Fatalf("URLsDeleted = %d, want 1", result.URLsDeleted)
	}

	count, err := module.Directory.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("Count = %d, want 1", count)
	}
}
