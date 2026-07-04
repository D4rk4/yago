package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
)

func TestLoadRuntimeSettingsOpenError(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, _, _, err := loadRuntimeSettings(
		context.Background(), v, testConfig(t), nil,
	); err == nil {
		t.Fatal("loadRuntimeSettings should fail on a closed store")
	}
}

func TestSettingsSourceReportsStoreErrors(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	store, err := settingsstore.Open(v)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}
	src := newSettingsSource(store, testConfig(t), newRuntimeToggles(testConfig(t)), nil)
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	ctx := context.Background()
	if _, err := src.Update(ctx, adminui.SettingsChange{
		Key: settingKeyPublicSearchPortal, Value: settingBoolTrue,
	}); err == nil {
		t.Fatal("set should surface the store error")
	}
	if _, err := src.Update(ctx, adminui.SettingsChange{
		Key: settingKeyPublicSearchPortal, Reset: true,
	}); err == nil {
		t.Fatal("reset should surface the store error")
	}
}

func TestSettingsSourceUpdateToleratesNilTogglesAndRecorder(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	store, err := settingsstore.Open(v)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}
	src := newSettingsSource(store, testConfig(t), nil, nil)

	res, err := src.Update(context.Background(), adminui.SettingsChange{
		Key: settingKeyPublicSearchPortal, Value: settingBoolTrue,
	})
	if err != nil || !res.OK {
		t.Fatalf("update = %+v err=%v", res, err)
	}
}

func TestAdminSettingOptionsEmpty(t *testing.T) {
	if got := adminSettingOptions(nil); got != nil {
		t.Fatalf("adminSettingOptions(nil) = %v, want nil", got)
	}
}
