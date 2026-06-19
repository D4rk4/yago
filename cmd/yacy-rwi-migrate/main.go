// Command yacy-rwi-migrate rewrites stored RWI postings into the current
// on-disk encoding. It reads the storage location from the same environment
// as yacy-rwi-node, is safe to interrupt, and can be re-run: postings already
// in the current encoding are left untouched.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nikitakarpei/yacy-rwi-node/internal/infrastructure"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migration terminated", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if err := infrastructure.ConfigureLogging(os.Getenv); err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}

	path := infrastructure.StoragePath(os.Getenv)
	storage, err := infrastructure.OpenBboltStorage(path, 0)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer closeStorage(storage)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("migration started", "path", path)
	report, err := storage.MigrateRWIPostings(ctx)
	slog.Info(
		"migration finished",
		"scanned", report.Scanned,
		"rewritten", report.Rewritten,
	)
	if err != nil {
		return fmt.Errorf("migrate rwi postings: %w", err)
	}

	return nil
}

func closeStorage(storage *infrastructure.BboltStorage) {
	if err := storage.Close(); err != nil {
		slog.Error("storage close failed", "error", err)
	}
}
