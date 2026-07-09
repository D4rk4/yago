package yagonode

import (
	"context"
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// consoleAdminSources bundles the console's runtime write surfaces so the ops
// mux can receive them as a single dependency.
type consoleAdminSources struct {
	settings    *settingsSource
	binding     *bindingSource
	security    *securitySource
	restart     func()
	perfHistory adminui.PerformanceHistorySource
}

// loadRuntimeSettings opens the durable settings store, layers any stored
// overrides onto the environment configuration, and returns the console's write
// sources (built over the unmodified environment defaults) and the effective
// configuration used to assemble the node.
func loadRuntimeSettings(
	ctx context.Context,
	storage *vault.Vault,
	config nodeConfig,
	recorder *events.Recorder,
) (consoleAdminSources, *runtimeToggles, nodeConfig, error) {
	store, err := settingsstore.Open(storage)
	if err != nil {
		return consoleAdminSources{}, nil, config, fmt.Errorf("open runtime settings: %w", err)
	}
	overrides, err := store.All(ctx)
	if err != nil {
		return consoleAdminSources{}, nil, config, fmt.Errorf("load runtime settings: %w", err)
	}

	envConfig := config
	config = applyRuntimeSettingOverrides(config, overrides)
	config = applyBindOverrides(config, overrides)
	config, err = resolvePeerIdentity(ctx, storage, config)
	if err != nil {
		return consoleAdminSources{}, nil, config, err
	}
	toggles := newRuntimeToggles(config)
	toggles.SetQuotaSink(storage.SetQuota)

	sources := consoleAdminSources{
		settings: newSettingsSource(store, envConfig, toggles, recorder),
		binding:  newBindingSource(store, envConfig, recorder),
	}

	return sources, toggles, config, nil
}

// settingsSource adapts the durable settings store and the runtime setting
// whitelist to the console's editable Configuration surface. It resolves each
// setting against its environment default, persists overrides, and records a
// config event on every change.
type settingsSource struct {
	store       *settingsstore.Store
	definitions []settingDefinition
	envConfig   nodeConfig
	toggles     *runtimeToggles
	recorder    *events.Recorder
}

func newSettingsSource(
	store *settingsstore.Store,
	envConfig nodeConfig,
	toggles *runtimeToggles,
	recorder *events.Recorder,
) *settingsSource {
	return &settingsSource{
		store:       store,
		definitions: allRuntimeSettingDefinitions(),
		envConfig:   envConfig,
		toggles:     toggles,
		recorder:    recorder,
	}
}

func (s *settingsSource) Settings(ctx context.Context) adminui.SettingsView {
	items := make([]adminui.SettingItem, 0, len(s.definitions))
	for _, def := range s.definitions {
		items = append(items, s.item(ctx, def))
	}

	return adminui.SettingsView{Items: items}
}

func (s *settingsSource) item(ctx context.Context, def settingDefinition) adminui.SettingItem {
	value := def.defaultValue(s.envConfig)
	overridden := false

	if stored, set, err := s.store.Get(ctx, def.key); err == nil && set {
		if normalized, normErr := def.normalize(stored); normErr == nil {
			value, overridden = normalized, true
		}
	}

	return adminui.SettingItem{
		Key:             def.key,
		Title:           def.title,
		Description:     def.description,
		Value:           value,
		Overridden:      overridden,
		RestartRequired: def.restartRequired(),
		Options:         adminSettingOptions(def.options),
		Category:        settingCategory(def.key),
		Boolean:         isBooleanSettingOptions(def.options),
	}
}

// isBooleanSettingOptions reports whether a setting's choices are exactly the
// Enabled/Disabled boolean pair, so the console renders it as a checkbox instead
// of a two-option dropdown.
func isBooleanSettingOptions(options []settingOption) bool {
	return len(options) == 2 &&
		options[0].value == settingBoolTrue &&
		options[1].value == settingBoolFalse
}

// settingCategory maps a setting key to the console tab that groups it, so the
// Configuration page presents related knobs together instead of one flat sheet.
func settingCategory(key string) string {
	switch {
	case strings.HasPrefix(key, "web.fallback."):
		return "Web fallback"
	case strings.HasPrefix(key, "search."):
		return "Search"
	case strings.HasPrefix(key, "storage."):
		return "Storage"
	case strings.HasPrefix(key, "swarm."):
		return "Swarm"
	case strings.HasPrefix(key, "network."):
		return "Network & peers"
	case strings.HasPrefix(key, "autocrawler."),
		strings.HasPrefix(key, "crawl."):
		return "Crawler"
	case strings.HasPrefix(key, "extract."):
		return "Extraction"
	case strings.HasPrefix(key, "metrics."):
		return "Monitoring"
	case strings.HasPrefix(key, "portal."),
		strings.HasPrefix(key, "web.robots."),
		key == "public.base.url",
		key == "https.redirect":
		return "Public portal"
	default:
		return "General"
	}
}

func (s *settingsSource) Update(
	ctx context.Context,
	change adminui.SettingsChange,
) (adminui.SettingsResult, error) {
	def, ok := s.definition(change.Key)
	if !ok {
		return adminui.SettingsResult{Message: "Unknown setting."}, nil
	}

	if change.Reset {
		return s.reset(ctx, def)
	}

	return s.set(ctx, def, change.Value)
}

func (s *settingsSource) set(
	ctx context.Context,
	def settingDefinition,
	raw string,
) (adminui.SettingsResult, error) {
	value, ok := normalizeSetting(def, raw)
	if !ok {
		return adminui.SettingsResult{Message: "Invalid value for " + def.title + "."}, nil
	}

	if err := s.store.Set(ctx, def.key, value); err != nil {
		return adminui.SettingsResult{}, fmt.Errorf("store setting %q: %w", def.key, err)
	}
	s.applyLive(def, value)
	s.record(def, "set to "+value)

	return adminui.SettingsResult{
		OK:              true,
		Message:         def.title + " updated.",
		RestartRequired: def.restartRequired(),
	}, nil
}

func (s *settingsSource) reset(
	ctx context.Context,
	def settingDefinition,
) (adminui.SettingsResult, error) {
	if err := s.store.Unset(ctx, def.key); err != nil {
		return adminui.SettingsResult{}, fmt.Errorf("clear setting %q: %w", def.key, err)
	}
	s.applyLive(def, def.defaultValue(s.envConfig))
	s.record(def, "reset to the environment default")

	return adminui.SettingsResult{
		OK:              true,
		Message:         def.title + " reset to the environment default.",
		RestartRequired: def.restartRequired(),
	}, nil
}

func (s *settingsSource) definition(key string) (settingDefinition, bool) {
	for _, def := range s.definitions {
		if def.key == key {
			return def, true
		}
	}

	return settingDefinition{}, false
}

func (s *settingsSource) applyLive(def settingDefinition, value string) {
	if def.applyLive == nil || s.toggles == nil {
		return
	}
	def.applyLive(s.toggles, value)
}

func (s *settingsSource) record(def settingDefinition, detail string) {
	if s.recorder == nil {
		return
	}

	s.recorder.Record(
		events.SeverityInfo,
		events.CategoryConfig,
		"settings.updated",
		fmt.Sprintf("runtime setting %q %s", def.key, detail),
	)
}

func normalizeSetting(def settingDefinition, raw string) (string, bool) {
	value, err := def.normalize(raw)
	if err != nil {
		return "", false
	}

	return value, true
}

func adminSettingOptions(options []settingOption) []adminui.SettingOption {
	if len(options) == 0 {
		return nil
	}

	out := make([]adminui.SettingOption, 0, len(options))
	for _, option := range options {
		out = append(out, adminui.SettingOption{Value: option.value, Label: option.label})
	}

	return out
}
