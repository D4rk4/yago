package adminui

import "context"

// SettingOption is one selectable value for a runtime setting rendered as a
// choice. Free-text settings carry no options.
type SettingOption struct {
	Value string
	Label string
}

// SettingItem is one operator-overridable runtime setting shown in the
// Configuration section's editable surface. Value is the effective value
// (override when present, otherwise the environment-derived default).
type SettingItem struct {
	Key             string
	Title           string
	Description     string
	Value           string
	Overridden      bool
	RestartRequired bool
	Options         []SettingOption
}

// SettingsView is the editable runtime-settings subset of the configuration.
type SettingsView struct {
	Items []SettingItem
}

// SettingsChange is a single runtime-setting update submitted from the console.
// Reset clears the override so the setting falls back to the environment.
type SettingsChange struct {
	Key   string
	Value string
	Reset bool
}

// SettingsResult reports the outcome of applying a runtime-setting change. OK is
// false for a rejected change, in which case Message is a display-safe reason.
type SettingsResult struct {
	OK              bool
	Message         string
	RestartRequired bool
}

// SettingsSource reads and writes the operator-overridable runtime settings that
// layer over the environment-derived configuration. A nil provider leaves the
// Configuration section read-only.
type SettingsSource interface {
	Settings(ctx context.Context) SettingsView
	Update(ctx context.Context, change SettingsChange) (SettingsResult, error)
}
