package yagonode

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
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

func TestBuildConfigViewDerivesWebFallbackFromPrivacy(t *testing.T) {
	for _, test := range []struct {
		name     string
		fallback webFallbackConfig
		expected string
	}{
		{
			name: "explicit ignores disabled legacy flag",
			fallback: webFallbackConfig{
				Enabled: false, Privacy: webFallbackPrivacyExplicit,
			},
			expected: "Web fallbackEnabled",
		},
		{
			name: "enabled policy",
			fallback: webFallbackConfig{
				Privacy: webFallbackPrivacyEnabled,
			},
			expected: "Web fallbackEnabled",
		},
		{
			name: "disabled ignores enabled legacy flag",
			fallback: webFallbackConfig{
				Enabled: true, Privacy: webFallbackPrivacyDisabled,
			},
			expected: "Web fallbackDisabled",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			flat := flattenConfig(buildConfigView(nodeConfig{WebFallback: test.fallback}))
			if !strings.Contains(flat, test.expected) {
				t.Fatalf("config view = %q, want %q", flat, test.expected)
			}
		})
	}
}

func TestBuildConfigViewShowsWebSearchTiming(t *testing.T) {
	parallel := flattenConfig(buildConfigView(nodeConfig{WebFallback: webFallbackConfig{
		Trigger: webFallbackTriggerParallel,
	}}))
	if !strings.Contains(parallel, "Web search timingAlongside local and peers") {
		t.Fatalf("parallel config view = %q", parallel)
	}
	miss := flattenConfig(buildConfigView(nodeConfig{}))
	if !strings.Contains(miss, "Web search timingAfter local and peer miss") {
		t.Fatalf("miss config view = %q", miss)
	}
}
