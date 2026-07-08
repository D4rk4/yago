package adminui

import (
	"context"
	"strings"
)

// SettingOption is one selectable value for a runtime setting rendered as a
// choice. Free-text settings carry no options.
type SettingOption struct {
	Value string
	Label string
}

// SettingItem is one operator-overridable runtime setting shown in the
// Configuration section's editable surface. Value is the effective value
// (override when present, otherwise the environment-derived default). Category
// groups related settings into a tab on the console; an empty category falls
// into "General".
type SettingItem struct {
	Key             string
	Title           string
	Description     string
	Value           string
	Overridden      bool
	RestartRequired bool
	Options         []SettingOption
	Category        string
	// Boolean marks an Enabled/Disabled setting so the console renders it as a
	// checkbox rather than a two-option dropdown.
	Boolean bool
}

// SettingsView is the editable runtime-settings subset of the configuration.
type SettingsView struct {
	Items []SettingItem
}

// SettingGroup is a category of runtime settings, rendered as one tab.
type SettingGroup struct {
	ID    string
	Title string
	Items []SettingItem
}

// settingGroupOrder fixes the tab order so the console layout is stable
// regardless of the order the settings source emits its items. Unlisted
// categories follow in first-seen order.
var settingGroupOrder = []string{
	"General", "Search", "Swarm", "Crawler",
	"Web fallback", "Extraction", "Monitoring", "Network & peers",
}

// groupSettings buckets the flat item list into ordered categories for tabbed
// display, preserving each item's order within its category.
func groupSettings(items []SettingItem) []SettingGroup {
	byCategory := map[string][]SettingItem{}
	firstSeen := make([]string, 0, len(items))
	for _, item := range items {
		category := item.Category
		if category == "" {
			category = "General"
		}
		if _, ok := byCategory[category]; !ok {
			firstSeen = append(firstSeen, category)
		}
		byCategory[category] = append(byCategory[category], item)
	}

	groups := make([]SettingGroup, 0, len(byCategory))
	emitted := map[string]bool{}
	appendGroup := func(category string) {
		if emitted[category] {
			return
		}
		if bucket, ok := byCategory[category]; ok {
			groups = append(groups, SettingGroup{
				ID: slugify(category), Title: category, Items: bucket,
			})
			emitted[category] = true
		}
	}
	for _, category := range settingGroupOrder {
		appendGroup(category)
	}
	for _, category := range firstSeen {
		appendGroup(category)
	}

	return groups
}

// slugify turns a category label into a stable DOM id fragment.
func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case !prevDash && b.Len() > 0:
			b.WriteByte('-')
			prevDash = true
		}
	}

	return strings.Trim(b.String(), "-")
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
