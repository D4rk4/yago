package yagonode

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
)

func TestLoggingLevelEnvironmentAndSettingUseCanonicalValues(t *testing.T) {
	definition := indexSettingDefinitions()[settingKeyLoggingLevel]
	for _, test := range []struct {
		raw      string
		expected string
	}{
		{raw: "debug", expected: "DEBUG"},
		{raw: " INFO ", expected: "INFO"},
		{raw: "warn", expected: "WARN"},
		{raw: "ERROR", expected: "ERROR"},
	} {
		level, err := parseLoggingLevel(test.raw)
		if err != nil || formatLoggingLevel(level) != test.expected {
			t.Errorf("environment level %q = %s, %v, want %s", test.raw, level, err, test.expected)
		}
		normalized, err := definition.normalize(test.raw)
		if err != nil || normalized != test.expected {
			t.Errorf("Admin level %q = %q, %v, want %s", test.raw, normalized, err, test.expected)
		}
	}
	for _, raw := range []string{"TRACE", "debug-1", "", "verbose"} {
		if _, err := definition.normalize(raw); err == nil {
			t.Errorf("Admin accepted %q", raw)
		}
	}
	if _, err := parseLoggingLevel("TRACE"); err == nil {
		t.Fatal("environment accepted TRACE")
	}
	if level, err := parseLoggingLevel(""); err != nil || level != slog.LevelInfo {
		t.Fatalf("empty environment level = %s, %v", level, err)
	}
	config, err := loadNodeConfig(envFrom(map[string]string{envLogLevel: "warn"}))
	if err != nil || config.LoggingLevel != slog.LevelWarn {
		t.Fatalf("node logging bootstrap = %s, %v", config.LoggingLevel, err)
	}
	if _, err := loadNodeConfig(envFrom(map[string]string{envLogLevel: "TRACE"})); err == nil {
		t.Fatal("node bootstrap accepted TRACE")
	}
	if settingCategory(settingKeyLoggingLevel) != "Monitoring" || definition.restartRequired() {
		t.Fatalf(
			"logging setting category/restart = %q/%t",
			settingCategory(settingKeyLoggingLevel),
			definition.restartRequired(),
		)
	}
}

func TestConfigureLoggingUsesMutableProcessLevel(t *testing.T) {
	original := processLoggingLevel.Level()
	t.Cleanup(func() { processLoggingLevel.Set(original) })
	if err := configureLogging(func(string) string { return "WARN" }); err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	if processLoggingLevel.Level() != slog.LevelWarn {
		t.Fatalf("process logging level = %s", processLoggingLevel.Level())
	}
}

func TestLoggingLevelStartupOverrideAndLiveResetReachProcessLevel(t *testing.T) {
	original := processLoggingLevel.Level()
	t.Cleanup(func() { processLoggingLevel.Set(original) })
	path := filepath.Join(t.TempDir(), "logging-settings.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open initial storage: %v", err)
	}
	store, err := settingsstore.Open(storage)
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}
	if err := store.Set(t.Context(), settingKeyLoggingLevel, "DEBUG"); err != nil {
		t.Fatalf("store logging override: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close initial storage: %v", err)
	}
	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	config := testConfig(t)
	config.LoggingLevel = slog.LevelWarn
	sources, toggles, effective, err := loadRuntimeSettings(t.Context(), storage, config, nil)
	if err != nil {
		t.Fatalf("load runtime settings: %v", err)
	}
	attachRuntimeLogging(toggles)
	item := settingViewItem(t, sources.settings.Settings(t.Context()), settingKeyLoggingLevel)
	if item.Category != "Monitoring" || item.Value != "DEBUG" || item.RestartRequired ||
		len(item.Options) != 4 {
		t.Fatalf("logging setting view = %+v", item)
	}
	if effective.LoggingLevel != slog.LevelDebug || processLoggingLevel.Level() != slog.LevelDebug {
		t.Fatalf(
			"startup logging level = %s/%s",
			effective.LoggingLevel,
			processLoggingLevel.Level(),
		)
	}
	result, err := sources.settings.Update(context.Background(), adminui.SettingsChange{
		Key: settingKeyLoggingLevel, Value: "ERROR",
	})
	if err != nil || !result.OK || result.RestartRequired ||
		processLoggingLevel.Level() != slog.LevelError {
		t.Fatalf(
			"live logging update = %+v, %v, level %s",
			result,
			err,
			processLoggingLevel.Level(),
		)
	}
	result, err = sources.settings.Update(context.Background(), adminui.SettingsChange{
		Key: settingKeyLoggingLevel, Reset: true,
	})
	if err != nil || !result.OK || result.RestartRequired ||
		processLoggingLevel.Level() != slog.LevelWarn {
		t.Fatalf("logging reset = %+v, %v, level %s", result, err, processLoggingLevel.Level())
	}
}
