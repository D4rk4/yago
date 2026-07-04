package yagonode

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/adminui"
)

func flattenConfig(view adminui.ConfigView) string {
	var builder strings.Builder
	for _, group := range view.Groups {
		builder.WriteString(group.Title)
		for _, setting := range group.Settings {
			builder.WriteString(setting.Name)
			builder.WriteString(setting.Value)
		}
	}

	return builder.String()
}

func TestBuildConfigViewRedactsSecrets(t *testing.T) {
	flat := flattenConfig(buildConfigView(nodeConfig{
		Name:         "peer-1",
		SearchAPIKey: "super-secret-key",
		Admin:        adminConfig{Username: "root", Password: "hunter2"},
	}))

	if strings.Contains(flat, "super-secret-key") || strings.Contains(flat, "hunter2") {
		t.Fatal("secrets must never appear in the config view")
	}
	if !strings.Contains(flat, "Configured") {
		t.Fatal("expected set secrets to render as Configured")
	}
	if !strings.Contains(flat, "peer-1") || !strings.Contains(flat, "root") {
		t.Fatal("expected non-secret values to be shown")
	}
}

func TestBuildConfigViewMarksUnsetValues(t *testing.T) {
	flat := flattenConfig(buildConfigView(nodeConfig{}))

	if !strings.Contains(flat, "Not set") {
		t.Fatal("expected unset secrets to render as Not set")
	}
	if !strings.Contains(flat, "Not configured") {
		t.Fatal("expected an unset admin username to render as Not configured")
	}
	if !strings.Contains(flat, "Unlimited") {
		t.Fatal("expected a zero storage quota to render as Unlimited")
	}
}
