package infrastructure

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

const EnvLogLevel = "LOG_LEVEL"

func ConfigureLogging(getenv func(string) string) error {
	level := slog.LevelInfo
	if raw := strings.TrimSpace(getenv(EnvLogLevel)); raw != "" {
		if err := level.UnmarshalText([]byte(strings.ToUpper(raw))); err != nil {
			return fmt.Errorf("%s: %w", EnvLogLevel, err)
		}
	}

	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))

	return nil
}
