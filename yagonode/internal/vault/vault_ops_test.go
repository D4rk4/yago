package vault_test

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var errCapability = errors.New("capability failure")

// capableEngine embeds doubleEngine to inherit the full Engine surface and adds
// the optional Compact/GrowShards capabilities with injectable results, so the
// success and error branches of Vault.Compact/Vault.GrowShards can be driven.
type capableEngine struct {
	*doubleEngine
	compactResult vault.CompactResult
	compactErr    error
	growSplits    int
	growErr       error
}

func (e *capableEngine) Compact(context.Context) (vault.CompactResult, error) {
	return e.compactResult, e.compactErr
}

func (e *capableEngine) GrowShards(context.Context, int) (int, error) {
	return e.growSplits, e.growErr
}

func openCapable(t *testing.T, engine *capableEngine) *vault.Vault {
	t.Helper()
	engine.doubleEngine = &doubleEngine{buckets: map[vault.Name]map[string][]byte{}}
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("new capable vault: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close capable vault: %v", err)
		}
	})

	return v
}

func openLiveDouble(t *testing.T) *vault.Vault {
	t.Helper()
	v, err := openDouble()
	if err != nil {
		t.Fatalf("openDouble: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	return v
}

func openClosedDouble(t *testing.T) *vault.Vault {
	t.Helper()
	v, err := openDouble()
	if err != nil {
		t.Fatalf("openDouble: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	return v
}

func TestVaultCompactCoversAllBranches(t *testing.T) {
	ctx := context.Background()

	var nilVault *vault.Vault
	if _, err := nilVault.Compact(ctx); err == nil {
		t.Fatal("Compact on nil vault succeeded, want error")
	}
	if _, err := openClosedDouble(t).Compact(ctx); err == nil {
		t.Fatal("Compact on closed vault succeeded, want error")
	}

	noop, err := openLiveDouble(t).Compact(ctx)
	if err != nil || noop != (vault.CompactResult{}) {
		t.Fatalf("Compact without capability = %+v, %v, want zero/nil", noop, err)
	}

	want := vault.CompactResult{ShardsCompacted: 3, BytesReclaimed: 4096}
	got, err := openCapable(t, &capableEngine{compactResult: want}).Compact(ctx)
	if err != nil || got != want {
		t.Fatalf("Compact = %+v, %v, want %+v/nil", got, err, want)
	}

	failing := openCapable(t, &capableEngine{compactErr: errCapability})
	if _, err := failing.Compact(ctx); err == nil {
		t.Fatal("Compact with an engine error succeeded, want error")
	}
}

func TestVaultGrowShardsCoversAllBranches(t *testing.T) {
	ctx := context.Background()

	var nilVault *vault.Vault
	if _, err := nilVault.GrowShards(ctx, 1); err == nil {
		t.Fatal("GrowShards on nil vault succeeded, want error")
	}
	if _, err := openClosedDouble(t).GrowShards(ctx, 1); err == nil {
		t.Fatal("GrowShards on closed vault succeeded, want error")
	}

	noop, err := openLiveDouble(t).GrowShards(ctx, 4)
	if err != nil || noop != 0 {
		t.Fatalf("GrowShards without capability = %d, %v, want 0/nil", noop, err)
	}

	got, err := openCapable(t, &capableEngine{growSplits: 5}).GrowShards(ctx, 4)
	if err != nil || got != 5 {
		t.Fatalf("GrowShards = %d, %v, want 5/nil", got, err)
	}

	failing := openCapable(t, &capableEngine{growErr: errCapability})
	if _, err := failing.GrowShards(ctx, 4); err == nil {
		t.Fatal("GrowShards with an engine error succeeded, want error")
	}
}
