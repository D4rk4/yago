package vault_test

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// TestVaultSetQuotaLiveCapacity: SetQuota changes the ceiling that AtCapacity
// reads each cycle, so lowering it below live usage reports at-capacity and
// raising it clears — no reopen, no restart (ADR-0037 D, the #5 fix).
func TestVaultSetQuotaLiveCapacity(t *testing.T) {
	engine := &doubleEngine{
		buckets: map[vault.Name]map[string][]byte{
			vault.Name("docs"): {"k": make([]byte, 1000)},
		},
		quotaBytes: 2000,
	}
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	ctx := context.Background()

	if atCap, err := v.AtCapacity(ctx); err != nil || atCap {
		t.Fatalf("under quota: atCap=%v err=%v, want false/nil", atCap, err)
	}

	v.SetQuota(500)
	if got := v.QuotaBytes(); got != 500 {
		t.Fatalf("quota after lowering = %d, want 500", got)
	}
	if atCap, err := v.AtCapacity(ctx); err != nil || !atCap {
		t.Fatalf("quota below usage: atCap=%v err=%v, want true/nil", atCap, err)
	}

	v.SetQuota(1 << 30)
	if atCap, err := v.AtCapacity(ctx); err != nil || atCap {
		t.Fatalf("quota raised above usage: atCap=%v err=%v, want false/nil", atCap, err)
	}
}

// TestVaultSetQuotaNilSafe: SetQuota on a nil or closed vault is a no-op, never
// a panic.
func TestVaultSetQuotaNilSafe(t *testing.T) {
	var nilVault *vault.Vault
	nilVault.SetQuota(1 << 30)
}
