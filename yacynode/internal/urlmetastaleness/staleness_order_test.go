package urlmetastaleness_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmetastaleness"
)

func openOrder(t *testing.T) (*boltvault.Vault, urlmetastaleness.Order) {
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

	order, err := urlmetastaleness.Open(vault)
	if err != nil {
		t.Fatalf("Open order: %v", err)
	}

	return vault, order
}

func store(
	t *testing.T,
	vault *boltvault.Vault,
	order urlmetastaleness.Order,
	hash yacymodel.Hash,
	freshness string,
) {
	t.Helper()

	if err := vault.Update(context.Background(), func(tx *boltvault.Txn) error {
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
	vault, order := openOrder(t)
	kept := yacymodel.WordHash("kept")
	gone := yacymodel.WordHash("gone")
	store(t, vault, order, kept, "20250101")
	store(t, vault, order, gone, "20200101")

	if err := vault.Update(context.Background(), func(tx *boltvault.Txn) error {
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
	vault, order := openOrder(t)

	if err := vault.Update(context.Background(), func(tx *boltvault.Txn) error {
		return order.URLPurged(tx, yacymodel.WordHash("absent"))
	}); err != nil {
		t.Fatalf("URLPurged: %v", err)
	}
}
