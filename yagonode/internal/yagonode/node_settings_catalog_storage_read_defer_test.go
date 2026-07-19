package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

func TestStorageReadDeferSettingPersistsForRestartAndResets(t *testing.T) {
	environment := nodeConfig{StorageReadDefer: 75 * time.Millisecond}
	source, _, _ := newTestSettingsSource(t, environment)

	result, err := source.Update(context.Background(), adminui.SettingsChange{
		Key:   settingKeyStorageReadDefer,
		Value: "-1s",
	})
	if err != nil || !result.OK || !result.RestartRequired {
		t.Fatalf("set result = %+v, error = %v", result, err)
	}
	item := settingViewItem(t, source.Settings(context.Background()), settingKeyStorageReadDefer)
	if item.Value != "-1s" || !item.Overridden || !item.PendingRestart {
		t.Fatalf("item = %+v, want overridden -1s", item)
	}

	result, err = source.Update(context.Background(), adminui.SettingsChange{
		Key:   settingKeyStorageReadDefer,
		Reset: true,
	})
	if err != nil || !result.OK || !result.RestartRequired {
		t.Fatalf("reset result = %+v, error = %v", result, err)
	}
	item = settingViewItem(t, source.Settings(context.Background()), settingKeyStorageReadDefer)
	if item.Value != "75ms" || item.Overridden || item.PendingRestart {
		t.Fatalf("item = %+v, want environment default 75ms", item)
	}
}

func TestStorageReadDeferSettingRejectsInvalidDuration(t *testing.T) {
	source, _, _ := newTestSettingsSource(t, nodeConfig{})
	result, err := source.Update(context.Background(), adminui.SettingsChange{
		Key:   settingKeyStorageReadDefer,
		Value: "later",
	})
	if err != nil || result.OK {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}
