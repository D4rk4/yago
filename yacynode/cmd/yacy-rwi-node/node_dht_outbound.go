package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/D4rk4/yago/yacynode/internal/dhtexchange"
	"github.com/D4rk4/yago/yacynode/internal/indextransfer"
	"github.com/D4rk4/yago/yacynode/internal/nodestatus"
	"github.com/D4rk4/yago/yacynode/internal/peerroster"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

const (
	dhtLocalRWICountUnavailableMessage   = "dht local rwi count unavailable"
	dhtStorageCapacityUnavailableMessage = "dht storage capacity unavailable"
)

type storageCapacity interface {
	AtCapacity(context.Context) (bool, error)
}

type dhtGateStateSource struct {
	publicReachable bool
	storage         storageCapacity
	postings        nodestatus.RWICounter
	roster          peerroster.Roster
}

type dhtOutboundRuntimeAssembly struct {
	ctx         context.Context
	config      nodeConfig
	storage     *vault.Vault
	nodeStorage nodeStorage
	report      nodestatus.Report
	roster      peerroster.Roster
	client      *http.Client
	observer    dhtexchange.DistributionObserver
}

func buildDHTOutboundRuntime(assembly dhtOutboundRuntimeAssembly) dhtOutboundProcess {
	self := assembly.report.SelfSeed(assembly.ctx)
	writer := indextransfer.NewHTTPPeerWriter(
		assembly.client,
		assembly.config.NetworkName,
		self,
	)
	distributor := dhtexchange.NewOutboundDistributor(
		dhtexchange.NewOutboundQueue(),
		indextransfer.NewRemoteRWICountProbe(
			assembly.client,
			assembly.config.NetworkName,
			self,
		),
		indextransfer.NewHandoff(writer, assembly.nodeStorage.urlDirectory),
	)
	scheduler := dhtexchange.NewOutboundScheduler(
		distributor,
		dhtexchange.NewOutboundRetryPolicy(dhtexchange.DefaultOutboundRetryConfig()),
		assembly.observer,
		dhtGateStateSource{
			publicReachable: assembly.config.AdvertiseHost != "",
			storage:         assembly.storage,
			postings:        assembly.nodeStorage.postings,
			roster:          assembly.roster,
		}.Snapshot,
		dhtexchange.OutboundSchedulerConfig{Gates: assembly.config.DHT.Gates},
	)

	return dhtOutboundProcess{cycle: scheduler, interval: assembly.config.DHT.Interval}
}

func (s dhtGateStateSource) Snapshot(ctx context.Context) dhtexchange.GateState {
	words, err := s.postings.RWICount(ctx)
	if err != nil {
		slog.WarnContext(ctx, dhtLocalRWICountUnavailableMessage, slog.Any("error", err))
	}

	atCapacity, err := s.storage.AtCapacity(ctx)
	storageAvailable := err == nil && !atCapacity
	if err != nil {
		slog.WarnContext(ctx, dhtStorageCapacityUnavailableMessage, slog.Any("error", err))
	}

	return dhtexchange.GateState{
		PublicReachable:  s.publicReachable,
		LocalPeerKnown:   true,
		ConnectedPeers:   len(s.roster.ReachablePeers(ctx)),
		LocalRWIWords:    words,
		StorageAvailable: storageAvailable,
	}
}
