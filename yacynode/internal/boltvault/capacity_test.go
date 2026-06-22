package boltvault_test

import (
	"context"
	"testing"
)

func TestQuotaAndUsedBytes(t *testing.T) {
	ctx := context.Background()
	vault := openVault(t, 4096)

	if vault.QuotaBytes() != 4096 {
		t.Fatalf("QuotaBytes = %d, want 4096", vault.QuotaBytes())
	}

	used, err := vault.UsedBytes(ctx)
	if err != nil {
		t.Fatalf("UsedBytes: %v", err)
	}
	if used < 0 {
		t.Fatalf("UsedBytes = %d, want non-negative", used)
	}
}
