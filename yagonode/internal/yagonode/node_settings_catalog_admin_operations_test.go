package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

func TestAdminRestartControlsSettingRequiresRestart(t *testing.T) {
	environment := nodeConfig{AdminRestartEnabled: true}
	source, _, _ := newTestSettingsSource(t, environment)

	result, err := source.Update(context.Background(), adminui.SettingsChange{
		Key:   settingKeyAdminRestartControls,
		Value: "false",
	})
	if err != nil || !result.OK || !result.RestartRequired {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	item := settingViewItem(
		t,
		source.Settings(context.Background()),
		settingKeyAdminRestartControls,
	)
	if item.Value != "false" || !item.Overridden || !item.PendingRestart {
		t.Fatalf("item = %+v, want pending disabled override", item)
	}

	effective := applyRuntimeSettingOverrides(environment, map[string]string{
		settingKeyAdminRestartControls: "false",
	})
	if effective.AdminRestartEnabled {
		t.Fatal("stored restart-controls override was not applied")
	}
}
