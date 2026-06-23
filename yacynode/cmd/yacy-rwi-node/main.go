package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

const (
	version = "1.83"

	receiveBatchCap       = 1000
	receiveBusyPauseSecs  = 30
	searchPostingsPerWord = 1000
	trustedSeedCapacity   = 4096

	evictionTargetFraction = 0.9
	evictionBatch          = 256

	serverReadHeaderTimeout = 10 * time.Second
	shutdownTimeout         = 15 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.ErrorContext(context.Background(), "node terminated", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	if err := configureLogging(os.Getenv); err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}

	settings, err := loadBootstrapSettings(os.Getenv)
	if err != nil {
		return fmt.Errorf("load bootstrap settings: %w", err)
	}

	announcing := len(settings.SeedlistURLs) > 0

	config, err := loadNodeConfig(os.Getenv, announcing)
	if err != nil {
		return fmt.Errorf("load node config: %w", err)
	}

	client := newOutboundHTTPClient()

	vault, err := boltvault.Open(config.StoragePath, config.StorageQuotaByte)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer closeVault(vault)

	publishStorageMetrics(vault)

	assembled, err := assembleNode(config, settings, vault, client)
	if err != nil {
		return fmt.Errorf("assemble node: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return serve(
		ctx,
		assembled,
		namedServer{
			"peer protocol",
			buildServer(config.PeerAddr, logHTTPRequests(instrumentHTTP(assembled.peerMux))),
		},
		namedServer{"ops", buildServer(config.OpsAddr, newOpsMux())},
	)
}

type namedServer struct {
	name   string
	server *http.Server
}

func buildServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
	}
}

func serve(ctx context.Context, assembled node, servers ...namedServer) error {
	ctx, cancel := context.WithCancel(ctx)

	var background sync.WaitGroup
	background.Add(2)
	go func() {
		defer background.Done()
		assembled.announcer.Run(ctx)
	}()
	go func() {
		defer background.Done()
		runEvictionLoop(ctx, assembled.sweeper)
	}()
	defer background.Wait()
	defer cancel()

	errs := make(chan error, len(servers))
	for _, s := range servers {
		go func(s namedServer) {
			slog.InfoContext(
				ctx,
				"serving",
				slog.String("service", s.name),
				slog.String("addr", s.server.Addr),
			)
			errs <- s.server.ListenAndServe()
		}(s)
	}

	select {
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return shutdown(servers)
		}

		return err
	case <-ctx.Done():
		return shutdown(servers)
	}
}

func shutdown(servers []namedServer) error {
	slog.InfoContext(context.Background(), "shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	var failures error
	for _, s := range servers {
		if err := s.server.Shutdown(ctx); err != nil {
			failures = errors.Join(failures, fmt.Errorf("shutdown %s: %w", s.name, err))
		}
	}

	return failures
}

func closeVault(vault *boltvault.Vault) {
	if err := vault.Close(); err != nil {
		slog.ErrorContext(context.Background(), "storage close failed", slog.Any("error", err))
	}
}
