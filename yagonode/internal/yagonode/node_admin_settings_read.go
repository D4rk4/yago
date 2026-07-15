package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

const runtimeSettingsUnavailable = "Stored runtime settings are unavailable."

func (s *settingsSource) settingsView(ctx context.Context) adminui.SettingsView {
	overrides, err := s.store.All(ctx)
	if err != nil {
		return adminui.SettingsView{Error: runtimeSettingsUnavailable}
	}

	items := make([]adminui.SettingItem, 0, len(s.definitions))
	for _, definition := range s.definitions {
		item, err := s.settingItem(definition, overrides)
		if err != nil {
			return adminui.SettingsView{Error: runtimeSettingsUnavailable}
		}
		items = append(items, item)
	}

	return adminui.SettingsView{Items: items}
}

func (s *settingsSource) settingItem(
	definition settingDefinition,
	overrides map[string]string,
) (adminui.SettingItem, error) {
	value := definition.defaultValue(s.envConfig)
	overridden := false
	stored, set := overrides[definition.key]
	if set {
		mode, authoritative, environment := decodeWebFallbackSetting(stored)
		switch {
		case definition.key == settingKeyWebFallbackPrivacy && authoritative && environment:
		case definition.key == settingKeyWebFallbackPrivacy && authoritative:
			value, overridden = string(mode), true
		default:
			normalized, err := definition.normalize(stored)
			if err != nil {
				return adminui.SettingItem{}, fmt.Errorf(
					"normalize stored runtime setting %q: %w",
					definition.key,
					err,
				)
			}
			value, overridden = normalized, true
		}
	}

	return adminui.SettingItem{
		Key:             definition.key,
		Title:           definition.title,
		Description:     definition.description,
		Value:           value,
		Overridden:      overridden,
		RestartRequired: definition.restartRequired(),
		PendingRestart: definition.restartRequired() &&
			value != definition.defaultValue(s.startupConfig),
		Options:  adminSettingOptions(definition.options),
		Category: settingCategory(definition.key),
		Boolean:  isBooleanSettingOptions(definition.options),
	}, nil
}
