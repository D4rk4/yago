package adminui

import "context"

// ConfigSetting is one effective configuration value, ready for display. Secret
// values are redacted by the provider before they reach the console.
type ConfigSetting struct {
	Name  string
	Value string
}

// ConfigGroup is a labelled set of related configuration settings.
type ConfigGroup struct {
	Title    string
	Settings []ConfigSetting
}

// ConfigView is the effective configuration grouped for the console's
// Configuration section.
type ConfigView struct {
	Groups []ConfigGroup
}

// ConfigSource reports the node's effective configuration for the console's
// Configuration section. A nil provider renders the section unavailable.
type ConfigSource interface {
	Config(ctx context.Context) ConfigView
}
