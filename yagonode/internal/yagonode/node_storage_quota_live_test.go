package yagonode

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
	"github.com/D4rk4/yago/yagonode/internal/shardvault"
)

func storageQuotaDefinition(t *testing.T) settingDefinition {
	t.Helper()
	for _, def := range extendedSettingDefinitions() {
		if def.key == "storage.quota" {
			return def
		}
	}
	t.Fatal("storage.quota missing from the catalog")

	return settingDefinition{}
}

// TestStorageQuotaSettingAppliesLive: storage.quota no longer requires a
// restart; applyLive routes the parsed ceiling through the toggles' quota sink
// (ADR-0037 D).
func TestStorageQuotaSettingAppliesLive(t *testing.T) {
	t.Parallel()

	def := storageQuotaDefinition(t)
	if def.restartRequired() {
		t.Fatal("storage quota must apply live, not require a restart")
	}

	toggles := &runtimeToggles{}
	var got int64 = -1
	toggles.SetQuotaSink(func(quotaBytes int64) { got = quotaBytes })
	def.applyLive(toggles, "768GB")
	if got != 768<<30 {
		t.Fatalf("applyLive(768GB) routed %d bytes, want %d", got, int64(768)<<30)
	}
}

// TestBootAppliesPersistedQuotaOverride reproduces #5: an operator persists a
// large storage quota, but the vault reopens on the next boot at the small env
// default. loadRuntimeSettings resolves the override, the boot step pushes it to
// the open vault, and a later admin change applies live — no restart, no reshard
// (ADR-0037 D). The persisted override is written by one vault instance and read
// by a fresh one over the same on-disk shards, exactly as a restart would.
func TestBootAppliesPersistedQuotaOverride(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "vault")

	persisted, err := shardvault.Open(dir, 1<<30)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	store, err := settingsstore.Open(persisted)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}
	if err := store.Set(ctx, "storage.quota", "768GB"); err != nil {
		t.Fatalf("persist quota override: %v", err)
	}
	if err := persisted.Close(); err != nil {
		t.Fatalf("close persisting vault: %v", err)
	}

	storage, err := shardvault.Open(dir, 1<<30) // reopens at the 1GB env default
	if err != nil {
		t.Fatalf("reopen vault: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	config := testConfig(t)
	config.StorageQuotaByte = 1 << 30 // env default
	recorder := events.NewRecorder(events.DefaultCapacity)
	_, toggles, effective, err := loadRuntimeSettings(ctx, storage, config, recorder)
	if err != nil {
		t.Fatalf("loadRuntimeSettings: %v", err)
	}
	if effective.StorageQuotaByte != 768<<30 {
		t.Fatalf("effective quota = %d, want the 768GB override", effective.StorageQuotaByte)
	}
	if got := storage.QuotaBytes(); got != 1<<30 {
		t.Fatalf("vault quota before boot apply = %d, want the 1GB env default", got)
	}

	storage.SetQuota(effective.StorageQuotaByte) // the bootNode step
	if got := storage.QuotaBytes(); got != 768<<30 {
		t.Fatalf("vault quota after boot = %d, want the 768GB override (#5)", got)
	}

	storageQuotaDefinition(t).applyLive(toggles, "500GB")
	if got := storage.QuotaBytes(); got != 500<<30 {
		t.Fatalf("vault quota after live change = %d, want 500GB", got)
	}
}
