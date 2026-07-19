package yagonode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/publicrobots"
)

// settingOption is one selectable value for a runtime setting rendered as a
// choice in the console.
type settingOption struct {
	value string
	label string
}

type settingDefinition struct {
	key          string
	title        string
	description  string
	options      []settingOption
	defaultValue func(config nodeConfig) string
	normalize    func(raw string) (string, error)
	apply        func(config nodeConfig, value string) nodeConfig
	applyLive    func(toggles *runtimeToggles, value string)
	sensitive    bool
}

// restartRequired reports whether a change to this setting only takes effect
// after a restart. Settings with a live-apply hook take effect immediately.
func (d settingDefinition) restartRequired() bool {
	return d.applyLive == nil
}

const (
	settingKeyPublicSearchPortal = "portal.enabled"
	settingKeyHTTPSRedirect      = "https.redirect"
	settingKeyPublicBaseURL      = "public.base.url"
)

const (
	settingBoolTrue  = "true"
	settingBoolFalse = "false"
)

func runtimeSettingDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         settingKeyPublicSearchPortal,
			title:       "Public search portal",
			description: "Serve the public search portal at the site root.",
			options:     boolSettingOptions(),
			defaultValue: func(config nodeConfig) string {
				return formatSettingBool(config.PublicSearchUIEnabled)
			},
			normalize: normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.PublicSearchUIEnabled = value == settingBoolTrue

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				toggles.SetPortalEnabled(value == settingBoolTrue)
			},
		},
		{
			key:         settingKeyHTTPSRedirect,
			title:       "HTTP to HTTPS redirect",
			description: "Redirect plain-HTTP requests to the https origin (TLS terminated in front).",
			options:     boolSettingOptions(),
			defaultValue: func(config nodeConfig) string {
				return formatSettingBool(config.HTTPSRedirect)
			},
			normalize: normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.HTTPSRedirect = value == settingBoolTrue

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				toggles.SetHTTPSRedirect(value == settingBoolTrue)
			},
		},
	}
}

// publicSurfaceDefinitions groups the live public-listener knobs added by the
// UI-GAP review (robots policy UI-15, portal name UI-21).
func publicSurfaceDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         "web.robots.policy",
			title:       "Robots policy",
			description: "What foreign crawlers may index on the public listener: hide the infinite search pages (default), allow everything, or close the whole site.",
			options: []settingOption{
				{value: string(publicrobots.PolicyNoSERP), label: "Hide search pages"},
				{value: string(publicrobots.PolicyOpen), label: "Allow everything"},
				{value: string(publicrobots.PolicyClosed), label: "Close the site"},
			},
			defaultValue: func(config nodeConfig) string {
				return string(publicrobots.ParsePolicy(config.RobotsPolicy))
			},
			normalize: normalizeRobotsPolicy,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.RobotsPolicy = value

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				toggles.SetRobotsPolicy(value)
			},
		},
		{
			key:         "portal.greeting",
			title:       "Portal name",
			description: "Display name shown on the public search portal (empty keeps the default brand).",
			defaultValue: func(config nodeConfig) string {
				return config.PortalGreeting
			},
			normalize: normalizePortalGreeting,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.PortalGreeting = value

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				toggles.SetPortalGreeting(value)
			},
		},
		{
			key:   settingKeyPublicBaseURL,
			title: "Public base URL",
			description: "Absolute public origin used in OpenSearch descriptors and " +
				"result links behind a reverse proxy (empty derives it from each request).",
			defaultValue: func(config nodeConfig) string {
				return config.PublicBaseURL
			},
			normalize: normalizePublicBaseURL,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.PublicBaseURL = value

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				toggles.SetPublicBaseURL(value)
			},
		},
	}
}

// allRuntimeSettingDefinitions is the console's full editable catalog: the
// live-appliable core plus the extended environment settings.
func allRuntimeSettingDefinitions() []settingDefinition {
	definitions := append(runtimeSettingDefinitions(), publicSurfaceDefinitions()...)
	definitions = append(definitions, networkAuthenticationSettingDefinitions()...)
	definitions = append(definitions, remoteCrawlSettingDefinitions()...)

	return append(definitions, extendedSettingDefinitions()...)
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
	parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid boolean %q: %w", raw, err)
	}

	return formatSettingBool(parsed), nil
}

// applyRuntimeSettingOverrides layers stored overrides onto the environment
// configuration at startup. Unknown or unparsable overrides are ignored so the
// environment default stands.
func applyRuntimeSettingOverrides(config nodeConfig, overrides map[string]string) nodeConfig {
	mode, authoritative, environment := decodeWebFallbackSetting(
		overrides[settingKeyWebFallbackPrivacy],
	)
	if !authoritative {
		raw, found := overrides[settingKeyLegacyWebFallbackTrigger]
		if found {
			if value, err := loadWebFallbackTrigger(
				func(string) string { return raw },
			); err == nil {
				config.WebFallback.Trigger = value
			}
		}
	}
	byKey := indexSettingDefinitions()
	for key, raw := range overrides {
		if authoritative && key == settingKeyWebFallbackPrivacy {
			continue
		}
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
	if authoritative && !environment {
		config.WebFallback.Privacy = mode
		config.WebFallback.Trigger = webFallbackTriggerMiss
	}

	return config
}

func indexSettingDefinitions() map[string]settingDefinition {
	definitions := allRuntimeSettingDefinitions()
	byKey := make(map[string]settingDefinition, len(definitions))
	for _, def := range definitions {
		byKey[def.key] = def
	}

	return byKey
}

// normalizeRobotsPolicy accepts only the three published robots policies.
func normalizeRobotsPolicy(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch publicrobots.Policy(value) {
	case publicrobots.PolicyOpen, publicrobots.PolicyNoSERP, publicrobots.PolicyClosed:
		return value, nil
	default:
		return "", fmt.Errorf("value must be no-serp, open, or closed")
	}
}

// normalizePortalGreeting bounds the portal name to one plain-text line.
func normalizePortalGreeting(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if len([]rune(value)) > 60 {
		return "", fmt.Errorf("value must be at most 60 characters")
	}
	if strings.ContainsAny(value, "<>\n\r") {
		return "", fmt.Errorf("value must be plain text without angle brackets")
	}

	return value, nil
}
