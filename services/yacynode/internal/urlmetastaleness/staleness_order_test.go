package urlmetastaleness_test

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/memvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmetastaleness"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

func openOrder(t *testing.T) (*vault.Vault, urlmetastaleness.StalenessRanking) {
	t.Helper()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	order, err := urlmetastaleness.Open(v)
	if err != nil {
		t.Fatalf("Open order: %v", err)
	}

	return v, order
}

func store(
	t *testing.T,
	v *vault.Vault,
	order urlmetastaleness.StalenessRanking,
	hash yacymodel.Hash,
	freshness string,
) {
	t.Helper()

	if err := v.Update(context.Background(), func(tx *vault.Txn) error {
		return order.URLStored(tx, hash, freshness)
	}); err != nil {
		t.Fatalf("URLStored: %v", err)
	}
}

func TestStalestURLsReturnsStalestFirst(t *testing.T) {
	vault, order := openOrder(t)
	fresh := yacymodel.WordHash("fresh")
	stale := yacymodel.WordHash("stale")
	middle := yacymodel.WordHash("middle")
	store(t, vault, order, fresh, "20260101")
	store(t, vault, order, stale, "20200101")
	store(t, vault, order, middle, "20230101")

	candidates, err := order.StalestURLs(context.Background(), 2)
	if err != nil {
		t.Fatalf("StalestURLs: %v", err)
	}
	if len(candidates) != 2 || candidates[0] != stale || candidates[1] != middle {
		t.Fatalf("candidates = %v, want [stale middle]", candidates)
	}
}

func TestStalestURLsTreatsMissingFreshnessAsStalest(t *testing.T) {
	vault, order := openOrder(t)
	dated := yacymodel.WordHash("dated")
	undated := yacymodel.WordHash("undated")
	store(t, vault, order, dated, "20200101")
	store(t, vault, order, undated, "")

	candidates, err := order.StalestURLs(context.Background(), 1)
	if err != nil {
		t.Fatalf("StalestURLs: %v", err)
	}
	if len(candidates) != 1 || candidates[0] != undated {
		t.Fatalf("candidates = %v, want [undated]", candidates)
	}
}

func TestStalestURLsZeroLimit(t *testing.T) {
	_, order := openOrder(t)

	candidates, err := order.StalestURLs(context.Background(), 0)
	if err != nil {
		t.Fatalf("StalestURLs: %v", err)
	}
	if candidates != nil {
		t.Fatalf("candidates = %v, want nil", candidates)
	}
}

func TestPurgedURLLeavesOrder(t *testing.T) {
	v, order := openOrder(t)
	kept := yacymodel.WordHash("kept")
	gone := yacymodel.WordHash("gone")
	store(t, v, order, kept, "20250101")
	store(t, v, order, gone, "20200101")

	if err := v.Update(context.Background(), func(tx *vault.Txn) error {
		return order.URLPurged(tx, gone)
	}); err != nil {
		t.Fatalf("URLPurged: %v", err)
	}

	candidates, err := order.StalestURLs(context.Background(), 10)
	if err != nil {
		t.Fatalf("StalestURLs: %v", err)
	}
	if len(candidates) != 1 || candidates[0] != kept {
		t.Fatalf("candidates = %v, want [kept]", candidates)
	}
}

func TestPurgeUnknownURLIsHarmless(t *testing.T) {
	v, order := openOrder(t)

	if err := v.Update(context.Background(), func(tx *vault.Txn) error {
		return order.URLPurged(tx, yacymodel.WordHash("absent"))
	}); err != nil {
		t.Fatalf("URLPurged: %v", err)
	}
}
