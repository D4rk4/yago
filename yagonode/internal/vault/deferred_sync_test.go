package vault_test

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// syncCapableEngine embeds doubleEngine to inherit the full Engine surface and
// adds the optional deferred-fsync capability with injectable results, so every
// branch of the Vault deferred-fsync forwarders can be driven.
type syncCapableEngine struct {
	*doubleEngine
	deferredEnabled bool
	syncErr         error
	setCalls        int
	lastSetEnabled  bool
	syncCalls       int
}

func (e *syncCapableEngine) SetDeferredFsync(enabled bool) {
	e.setCalls++
	e.lastSetEnabled = enabled
}

func (e *syncCapableEngine) SyncShards(context.Context) error {
	e.syncCalls++

	return e.syncErr
}

func (e *syncCapableEngine) DeferredFsyncEnabled() bool { return e.deferredEnabled }

func openSyncCapable(t *testing.T, engine *syncCapableEngine) *vault.Vault {
	t.Helper()
	engine.doubleEngine = &doubleEngine{buckets: map[vault.Name]map[string][]byte{}}
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("new sync-capable vault: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close sync-capable vault: %v", err)
		}
	})

	return v
}

func TestVaultSyncShardsCoversAllBranches(t *testing.T) {
	ctx := context.Background()

	var nilVault *vault.Vault
	if err := nilVault.SyncShards(ctx); err == nil {
		t.Fatal("SyncShards on nil vault succeeded, want error")
	}
	if err := openClosedDouble(t).SyncShards(ctx); err == nil {
		t.Fatal("SyncShards on closed vault succeeded, want error")
	}

	if err := openLiveDouble(t).SyncShards(ctx); err != nil {
		t.Fatalf("SyncShards without capability = %v, want nil (no-op)", err)
	}

	engine := &syncCapableEngine{}
	if err := openSyncCapable(t, engine).SyncShards(ctx); err != nil {
		t.Fatalf("SyncShards = %v, want nil", err)
	}
	if engine.syncCalls != 1 {
		t.Fatalf("SyncShards forwarded %d times, want 1", engine.syncCalls)
	}

	failing := openSyncCapable(t, &syncCapableEngine{syncErr: errCapability})
	if err := failing.SyncShards(ctx); err == nil {
		t.Fatal("SyncShards with an engine error succeeded, want error")
	}
}

func TestVaultSetDeferredFsyncCoversAllBranches(t *testing.T) {
	var nilVault *vault.Vault
	nilVault.SetDeferredFsync(true)

	openClosedDouble(t).SetDeferredFsync(true)
	openLiveDouble(t).SetDeferredFsync(true)

	engine := &syncCapableEngine{}
	openSyncCapable(t, engine).SetDeferredFsync(true)
	if engine.setCalls != 1 || !engine.lastSetEnabled {
		t.Fatalf("SetDeferredFsync forwarded calls=%d enabled=%v, want 1/true",
			engine.setCalls, engine.lastSetEnabled)
	}
}

func TestVaultDeferredFsyncEnabledCoversAllBranches(t *testing.T) {
	var nilVault *vault.Vault
	if nilVault.DeferredFsyncEnabled() {
		t.Fatal("nil vault reported deferred fsync enabled")
	}
	if openClosedDouble(t).DeferredFsyncEnabled() {
		t.Fatal("closed vault reported deferred fsync enabled")
	}
	if openLiveDouble(t).DeferredFsyncEnabled() {
		t.Fatal("engine without the capability reported deferred fsync enabled")
	}
	if !openSyncCapable(t, &syncCapableEngine{deferredEnabled: true}).DeferredFsyncEnabled() {
		t.Fatal("capable engine reported deferred fsync disabled")
	}
	if openSyncCapable(t, &syncCapableEngine{deferredEnabled: false}).DeferredFsyncEnabled() {
		t.Fatal("capable engine reported deferred fsync enabled when off")
	}
}
