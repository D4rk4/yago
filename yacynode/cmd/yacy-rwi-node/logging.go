package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

const envLogLevel = "LOG_LEVEL"

func configureLogging(getenv func(string) string) error {
	level := slog.LevelInfo
	if raw := strings.TrimSpace(getenv(envLogLevel)); raw != "" {
		if err := level.UnmarshalText([]byte(strings.ToUpper(raw))); err != nil {
			return fmt.Errorf("%s: %w", envLogLevel, err)
		}
	}

	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))

	return nil
}
