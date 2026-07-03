package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

var lookupEnv = os.Getenv

func main() {
	os.Exit(start())
}

func start() int {
	cfg, err := LoadServiceConfig(lookupEnv)
	if err != nil {
		slog.ErrorContext(context.Background(), "textindexer config invalid", slog.Any("error", err))
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := RunService(ctx, cfg); err != nil {
		slog.ErrorContext(ctx, "textindexer failed", slog.Any("error", err))
		return 1
	}
	return 0
}
