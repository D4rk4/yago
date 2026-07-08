package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlmetrics"
)

func main() {
	if err := run(); err != nil {
		slog.ErrorContext(context.Background(), "crawler terminated", slog.Any("error", err))
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

	return RunService(ctx, cfg, crawlmetrics.New())
}
