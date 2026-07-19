package yagonode

import (
	"fmt"
	"log/slog"
	"os"
)

const envLogLevel = "LOG_LEVEL"

func configureLogging(getenv func(string) string) error {
	level, err := parseLoggingLevel(getenv(envLogLevel))
	if err != nil {
		return fmt.Errorf("%s: %w", envLogLevel, err)
	}
	processLoggingLevel.Set(level)
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: processLoggingLevel})
	slog.SetDefault(slog.New(handler))

	return nil
}
