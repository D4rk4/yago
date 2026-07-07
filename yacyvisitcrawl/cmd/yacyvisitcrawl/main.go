package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nikitakarpei/yacy-rwi-node/yacyvisitcrawl/internal/visitmetrics"
)

func main() {
	if err := run(); err != nil {
		slog.ErrorContext(context.Background(), "yacyvisitcrawl terminated", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	if err := configureLogging(os.Getenv); err != nil {
		return err
	}
	cfg, err := LoadServiceConfig(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return RunService(ctx, cfg, visitmetrics.New())
}
