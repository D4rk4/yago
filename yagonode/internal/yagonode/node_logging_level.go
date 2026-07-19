package yagonode

import (
	"fmt"
	"log/slog"
	"strings"
)

const settingKeyLoggingLevel = "logging.level"

var processLoggingLevel = new(slog.LevelVar)

type loggingBootstrap struct {
	queryMode queryLogMode
	level     slog.Level
}

func loadLoggingBootstrap(getenv func(string) string) (loggingBootstrap, error) {
	queryMode, err := parseQueryLogMode(getenv(envQueryLogMode))
	if err != nil {
		return loggingBootstrap{}, fmt.Errorf("%s: %w", envQueryLogMode, err)
	}
	level, err := parseLoggingLevel(getenv(envLogLevel))
	if err != nil {
		return loggingBootstrap{}, fmt.Errorf("%s: %w", envLogLevel, err)
	}

	return loggingBootstrap{queryMode: queryMode, level: level}, nil
}

func parseLoggingLevel(raw string) (slog.Level, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if value == "" {
		return slog.LevelInfo, nil
	}
	switch value {
	case "DEBUG":
		return slog.LevelDebug, nil
	case "INFO":
		return slog.LevelInfo, nil
	case "WARN":
		return slog.LevelWarn, nil
	case "ERROR":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("value must be DEBUG, INFO, WARN, or ERROR")
	}
}

func formatLoggingLevel(level slog.Level) string {
	switch level {
	case slog.LevelDebug:
		return "DEBUG"
	case slog.LevelWarn:
		return "WARN"
	case slog.LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

func normalizeLoggingLevel(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("value must be DEBUG, INFO, WARN, or ERROR")
	}
	level, err := parseLoggingLevel(raw)
	if err != nil {
		return "", err
	}

	return formatLoggingLevel(level), nil
}

func loggingLevelDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         settingKeyLoggingLevel,
			title:       "Log level",
			description: "Minimum severity written to the node log.",
			options: []settingOption{
				{value: "DEBUG", label: "Debug"},
				{value: "INFO", label: "Info"},
				{value: "WARN", label: "Warn"},
				{value: "ERROR", label: "Error"},
			},
			defaultValue: func(config nodeConfig) string {
				return formatLoggingLevel(config.LoggingLevel)
			},
			normalize: normalizeLoggingLevel,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.LoggingLevel, _ = parseLoggingLevel(value)

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				level, _ := parseLoggingLevel(value)
				toggles.ApplyLoggingLevel(level)
			},
		},
	}
}

func (t *runtimeToggles) SetLoggingLevelSink(sink func(slog.Level)) {
	t.loggingLevelSink.Store(loggingLevelSink(sink))
	sink(slog.Level(t.loggingLevel.Load()))
}

func (t *runtimeToggles) ApplyLoggingLevel(level slog.Level) {
	t.loggingLevel.Store(int64(level))
	if sink, ok := t.loggingLevelSink.Load().(loggingLevelSink); ok {
		sink(level)
	}
}

func attachRuntimeLogging(toggles *runtimeToggles) {
	toggles.SetLoggingLevelSink(processLoggingLevel.Set)
}

type loggingLevelSink func(slog.Level)
