package boltvault_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

func TestQuotaAndUsedBytes(t *testing.T) {
	ctx := context.Background()
	store, err := boltvault.Open(filepath.Join(t.TempDir(), "node.db"), 4096)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	if store.QuotaBytes() != 4096 {
		t.Fatalf("QuotaBytes = %d, want 4096", store.QuotaBytes())
	}

	used, err := store.UsedBytes(ctx)
	if err != nil {
		t.Fatalf("UsedBytes: %v", err)
	}
	if used < 0 {
		t.Fatalf("UsedBytes = %d, want non-negative", used)
	}
}
