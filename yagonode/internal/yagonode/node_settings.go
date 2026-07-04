package yagonode

import (
	"fmt"
	"strconv"
)

// settingOption is one selectable value for a runtime setting rendered as a
// choice in the console.
type settingOption struct {
	value string
	label string
}

// settingDefinition describes one operator-overridable runtime setting: how it
// reads from the environment default, how a submitted value is normalized and
// validated, and how a stored override layers onto the boot configuration. The
// whitelist deliberately excludes secrets, which are never stored here.
type settingDefinition struct {
	key             string
	title           string
	description     string
	restartRequired bool
	options         []settingOption
	defaultValue    func(config nodeConfig) string
	normalize       func(raw string) (string, error)
	apply           func(config nodeConfig, value string) nodeConfig
}

const settingKeyPublicSearchPortal = "portal.enabled"

const (
	settingBoolTrue  = "true"
	settingBoolFalse = "false"
)

func runtimeSettingDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:             settingKeyPublicSearchPortal,
			title:           "Public search portal",
			description:     "Serve the public search portal at the site root.",
			restartRequired: true,
			options:         boolSettingOptions(),
			defaultValue: func(config nodeConfig) string {
				return formatSettingBool(config.PublicSearchUIEnabled)
			},
			normalize: normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.PublicSearchUIEnabled = value == settingBoolTrue

				return config
			},
		},
	}
}

func boolSettingOptions() []settingOption {
	return []settingOption{
		{value: settingBoolTrue, label: "Enabled"},
		{value: settingBoolFalse, label: "Disabled"},
	}
}

func formatSettingBool(value bool) string {
	if value {
		return settingBoolTrue
	}

	return settingBoolFalse
}

func normalizeSettingBool(raw string) (string, error) {
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return "", fmt.Errorf("invalid boolean %q: %w", raw, err)
	}

	return formatSettingBool(parsed), nil
}

// applyRuntimeSettingOverrides layers stored overrides onto the environment
// configuration at startup. Unknown or unparsable overrides are ignored so the
// environment default stands.
func applyRuntimeSettingOverrides(config nodeConfig, overrides map[string]string) nodeConfig {
	byKey := indexSettingDefinitions()
	for key, raw := range overrides {
		def, ok := byKey[key]
		if !ok {
			continue
		}
		value, err := def.normalize(raw)
		if err != nil {
			continue
		}
		config = def.apply(config, value)
	}

	return config
}

func indexSettingDefinitions() map[string]settingDefinition {
	definitions := runtimeSettingDefinitions()
	byKey := make(map[string]settingDefinition, len(definitions))
	for _, def := range definitions {
		byKey[def.key] = def
	}

	return byKey
}
