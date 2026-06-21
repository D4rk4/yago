// Command yacy-rwi-node is the composition root: the only place where the api,
// core, and infrastructure layers are wired together.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/services"
	"github.com/nikitakarpei/yacy-rwi-node/internal/infrastructure"
)

const (
	version = "1.83"

	receiveBatchCap       = 1000
	receiveBusyPauseSecs  = 30
	searchPostingsPerWord = 1000
	trustedSeedCapacity   = 4096

	evictionHighWaterNum = 90
	evictionLowWaterNum  = 80
	evictionWaterDen     = 100
	evictionBatch        = 256

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
	if err := infrastructure.ConfigureLogging(os.Getenv); err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}

	bootstrap, err := infrastructure.LoadBootstrapSettings(os.Getenv)
	if err != nil {
		return fmt.Errorf("load bootstrap settings: %w", err)
	}

	announcing := len(bootstrap.SeedlistURLs()) > 0

	config, err := infrastructure.LoadNodeConfig(os.Getenv, announcing)
	if err != nil {
		return fmt.Errorf("load node config: %w", err)
	}

	client := infrastructure.NewOutboundHTTPClient()

	storage, err := infrastructure.OpenBboltStorage(config.StoragePath, config.StorageQuotaByte)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer closeStorage(storage)

	infrastructure.PublishBboltStats(storage)

	sweeper := services.NewRWIEvictionSweeper(
		storage,
		services.NewDropEvictionPolicy(storage),
		config.StorageQuotaByte*evictionHighWaterNum/evictionWaterDen,
		config.StorageQuotaByte*evictionLowWaterNum/evictionWaterDen,
		evictionBatch,
	)
	infrastructure.PublishEvictionStats(sweeper)
	sweeper.Trigger()

	status := services.NewRuntimeStatus(
		nodeIdentity(config),
		infrastructure.SystemClock{},
		storage,
		storage,
		version,
	)
	registry := services.NewTrustedSeedRegistry(trustedSeedCapacity)
	pinger := infrastructure.NewPeerBackPing(client, config.Hash, config.NetworkName)
	peers := services.NewPeerDirectory(pinger, registry, rand.Shuffle)

	mux := newPeerProtocolMux(config, status, peers, storage, sweeper)

	announcement := services.NewPeerAnnouncement(
		bootstrap,
		infrastructure.NewHTTPSeedlistFetcher(client),
		infrastructure.NewHTTPPeerGreeter(client, config.NetworkName),
		status,
		registry,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return serve(
		ctx,
		announcement,
		namedServer{
			"peer protocol",
			buildServer(
				config.PeerAddr,
				infrastructure.LogHTTPRequests(infrastructure.InstrumentHTTP(mux)),
			),
		},
		namedServer{"ops", buildServer(config.OpsAddr, infrastructure.NewOpsMux())},
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

func serve(
	ctx context.Context,
	announcement *services.PeerAnnouncement,
	servers ...namedServer,
) error {
	go announcement.Run(ctx)

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

func closeStorage(storage *infrastructure.BboltStorage) {
	if err := storage.Close(); err != nil {
		slog.ErrorContext(context.Background(), "storage close failed", slog.Any("error", err))
	}
}
