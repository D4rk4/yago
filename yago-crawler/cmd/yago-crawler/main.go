package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/D4rk4/yago/yago-crawler/internal/worker"
)

var version = "dev"

func main() {
	os.Exit(start())
}

func start() int {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		_, _ = fmt.Fprintln(os.Stdout, "yago-crawler "+version)
		return 0
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	configuration, err := loadServiceConfiguration(os.Getenv)
	if err != nil {
		slog.Error("crawler configuration invalid", slog.Any("error", err))
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runner, err := worker.Open(ctx, worker.Configuration{
		NodeRPCAddress: configuration.nodeRPCAddress,
		NodeAPIKey:     configuration.nodeAPIKey,
		DataDirectory:  configuration.dataDirectory,
		WorkerID:       configuration.workerID,
	})
	if err != nil {
		slog.ErrorContext(ctx, "crawler startup failed", slog.Any("error", err))
		return 1
	}
	defer runner.Close()
	if err := runner.Run(ctx); err != nil {
		if crawlerWasStoppedBySignal(ctx, err) {
			return 0
		}
		slog.ErrorContext(ctx, "crawler stopped with an error", slog.Any("error", err))
		return 1
	}
	return 0
}

func crawlerWasStoppedBySignal(ctx context.Context, runError error) bool {
	return ctx.Err() != nil && runError == context.Canceled
}
