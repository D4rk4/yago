package main

import (
	"context"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/api"
	"github.com/nikitakarpei/yacy-rwi-node/internal/core/services"
	"github.com/nikitakarpei/yacy-rwi-node/internal/infrastructure"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestRunRejectsInvalidConfig(t *testing.T) {
	t.Setenv(infrastructure.EnvPeerHash, "")
	if err := run(); err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestServeReturnsNilAfterCancel(t *testing.T) {
	storage, err := infrastructure.OpenBboltStorage(filepath.Join(t.TempDir(), "db"), 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	client := infrastructure.NewOutboundHTTPClient()
	bootstrap, err := infrastructure.LoadBootstrapSettings(func(string) string { return "" })
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	const network = "freeworld"
	hash := yacymodel.Hash("0123456789AB")
	identity := services.NewIdentity(
		hash,
		network,
		"node",
		"203.0.113.1",
		8090,
		yacymodel.ZeroFlags(),
	)
	status := services.NewRuntimeStatus(
		identity,
		infrastructure.SystemClock{},
		storage,
		storage,
		version,
	)
	registry := services.NewTrustedSeedRegistry(trustedSeedCapacity)
	peers := services.NewPeerDirectory(
		infrastructure.NewPeerBackPing(client, hash, network),
		registry,
		rand.Shuffle,
	)
	mux := api.NewPeerProtocolMux(
		identity,
		status,
		peers,
		services.NewRWIReceiver(storage, storage, receiveBatchCap, receiveBusyPauseSecs),
		services.NewURLReceiver(storage),
		services.NewSearcher(storage, storage, searchPostingsPerWord),
		services.NewCounter(storage, storage),
	)
	announcement := services.NewPeerAnnouncement(
		bootstrap,
		infrastructure.NewHTTPSeedlistFetcher(client),
		infrastructure.NewHTTPPeerGreeter(client, network),
		status,
		registry,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = serve(ctx, announcement,
		namedServer{"peer protocol", buildServer("127.0.0.1:0", mux)},
		namedServer{"ops", buildServer("127.0.0.1:0", infrastructure.NewOpsMux())},
	)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
}
