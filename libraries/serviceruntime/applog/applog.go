// Package applog installs the process-wide structured logger, emitting JSON to
// standard error at a level read from the LOG_LEVEL environment variable.
package applog

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

const envLogLevel = "LOG_LEVEL"

func Configure(getenv func(string) string) error {
	level := slog.LevelInfo
	if raw := strings.TrimSpace(getenv(envLogLevel)); raw != "" {
		if err := level.UnmarshalText([]byte(strings.ToUpper(raw))); err != nil {
			return fmt.Errorf("%s: %w", envLogLevel, err)
		}
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	return nil
}
