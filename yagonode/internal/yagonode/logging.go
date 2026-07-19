package yagonode

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

const envLogLevel = "LOG_LEVEL"

func configureLogging(getenv func(string) string) error {
	return configureLoggingTo(getenv, os.Stdout)
}

func configureLoggingTo(getenv func(string) string, output io.Writer) error {
	level, err := parseLoggingLevel(getenv(envLogLevel))
	if err != nil {
		return fmt.Errorf("%s: %w", envLogLevel, err)
	}
	processLoggingLevel.Set(level)
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: processLoggingLevel})
	slog.SetDefault(slog.New(handler))

	return nil
}
