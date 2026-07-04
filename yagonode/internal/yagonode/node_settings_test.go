package yagonode

import "testing"

func TestNormalizeSettingBool(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "true", want: "true"},
		{raw: "false", want: "false"},
		{raw: "1", want: "true"},
		{raw: "0", want: "false"},
		{raw: "maybe", wantErr: true},
		{raw: "", wantErr: true},
	} {
		got, err := normalizeSettingBool(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("normalizeSettingBool(%q) = %q, want error", tc.raw, got)
			}

			continue
		}
		if err != nil {
			t.Fatalf("normalizeSettingBool(%q): %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeSettingBool(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestApplyRuntimeSettingOverridesEnablesPortal(t *testing.T) {
	t.Parallel()

	config := applyRuntimeSettingOverrides(
		nodeConfig{PublicSearchUIEnabled: false},
		map[string]string{settingKeyPublicSearchPortal: "true"},
	)
	if !config.PublicSearchUIEnabled {
		t.Fatal("portal override not applied")
	}
}

func TestApplyRuntimeSettingOverridesDisablesPortal(t *testing.T) {
	t.Parallel()

	config := applyRuntimeSettingOverrides(
		nodeConfig{PublicSearchUIEnabled: true},
		map[string]string{settingKeyPublicSearchPortal: "false"},
	)
	if config.PublicSearchUIEnabled {
		t.Fatal("portal override did not disable the portal")
	}
}

func TestApplyRuntimeSettingOverridesIgnoresUnknownKey(t *testing.T) {
	t.Parallel()

	config := applyRuntimeSettingOverrides(
		nodeConfig{PublicSearchUIEnabled: true},
		map[string]string{"nonexistent.setting": "true"},
	)
	if !config.PublicSearchUIEnabled {
		t.Fatal("unknown override changed the configuration")
	}
}

func TestApplyRuntimeSettingOverridesIgnoresInvalidValue(t *testing.T) {
	t.Parallel()

	config := applyRuntimeSettingOverrides(
		nodeConfig{PublicSearchUIEnabled: true},
		map[string]string{settingKeyPublicSearchPortal: "maybe"},
	)
	if !config.PublicSearchUIEnabled {
		t.Fatal("invalid override changed the configuration; environment default should stand")
	}
}

func TestApplyRuntimeSettingOverridesEnablesHTTPSRedirect(t *testing.T) {
	t.Parallel()

	config := applyRuntimeSettingOverrides(
		nodeConfig{HTTPSRedirect: false},
		map[string]string{settingKeyHTTPSRedirect: "true"},
	)
	if !config.HTTPSRedirect {
		t.Fatal("https redirect override not applied")
	}
}

func TestRuntimeSettingDefinitionsIncludeHTTPSRedirect(t *testing.T) {
	t.Parallel()

	def, ok := indexSettingDefinitions()[settingKeyHTTPSRedirect]
	if !ok {
		t.Fatal("https redirect setting is not in the whitelist")
	}
	if def.restartRequired() {
		t.Fatal("https redirect should apply live, not require a restart")
	}
}

func TestRuntimeSettingDefinitionsIncludePortal(t *testing.T) {
	t.Parallel()

	def, ok := indexSettingDefinitions()[settingKeyPublicSearchPortal]
	if !ok {
		t.Fatal("public search portal setting is not in the whitelist")
	}
	if def.restartRequired() {
		t.Fatal("portal toggle should apply live, not require a restart")
	}
	if def.defaultValue(nodeConfig{PublicSearchUIEnabled: true}) != "true" {
		t.Fatal("portal default value should reflect the environment configuration")
	}
}
