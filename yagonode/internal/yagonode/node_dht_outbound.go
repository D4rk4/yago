package yagonode

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	dhtLocalRWICountUnavailableMessage   = "dht local rwi count unavailable"
	dhtStorageCapacityUnavailableMessage = "dht storage capacity unavailable"
)

type storageCapacity interface {
	AtCapacity(context.Context) (bool, error)
}

type dhtGateStateSource struct {
	reachability publicReachability
	storage      storageCapacity
	postings     nodestatus.RWICounter
	roster       peerroster.Roster
	crawl        crawlQueueDepthSource
	index        indexQueueDepthSource
}

type dhtOutboundRuntimeAssembly struct {
	ctx          context.Context
	config       nodeConfig
	storage      *vault.Vault
	nodeStorage  nodeStorage
	report       nodestatus.Report
	roster       peerroster.Roster
	client       *http.Client
	observer     dhtexchange.DistributionObserver
	events       nodeEventRecorder
	reachability publicReachability
	crawl        crawlQueueDepthSource
	index        indexQueueDepthSource
}

func buildDHTOutboundRuntime(assembly dhtOutboundRuntimeAssembly) dhtOutboundProcess {
	self := assembly.report.SelfSeed(assembly.ctx)
	access := configuredNetworkAccess(assembly.config, self.Hash)
	reachability := assembly.reachability
	if reachability == nil {
		reachability = newPublicEndpointSelfTest(
			assembly.client,
			assembly.config.NetworkName,
			self.Hash,
			assembly.config.PublicSelfTestURL,
			access,
		)
	}
	gateSource := dhtGateStateSource{
		reachability: reachability,
		storage:      assembly.storage,
		postings:     assembly.nodeStorage.postings,
		roster:       assembly.roster,
		crawl:        assembly.crawl,
		index:        assembly.index,
	}
	writer := indextransfer.NewHTTPPeerWriter(
		assembly.client,
		assembly.config.NetworkName,
		self,
		assembly.config.PeerHTTPSPreferred,
		access,
	)
	queue := dhtexchange.NewOutboundQueue()
	feeder := dhtexchange.NewOutboundFeeder(
		queue,
		dhtOutboundRWIWords{postings: assembly.nodeStorage.outboundPostings},
		assembly.nodeStorage.urlDirectory,
		assembly.roster.ReachablePeers,
		dhtexchange.OutboundFeederConfig{
			MaxWords:           1,
			MaxPostings:        dhtexchange.MaxChunkPostings,
			Redundancy:         assembly.config.DHT.Redundancy,
			PartitionExponent:  assembly.config.DHT.PartitionExponent,
			MinimumPeerAgeDays: assembly.config.DHT.MinimumPeerAgeDays,
		},
	)
	distributor := dhtexchange.NewConfirmingOutboundDistributor(
		queue,
		indextransfer.NewRemoteRWICountProbe(
			assembly.client,
			assembly.config.NetworkName,
			self,
			assembly.config.PeerHTTPSPreferred,
			access,
		),
		indextransfer.NewHandoff(writer, assembly.nodeStorage.urlDirectory),
		dhtOutboundRWIWords{postings: assembly.nodeStorage.outboundPostings},
	)
	scheduler := dhtexchange.NewOutboundScheduler(
		distributor,
		dhtexchange.NewOutboundRetryPolicy(dhtexchange.DefaultOutboundRetryConfig()),
		assembly.observer,
		gateSource.Snapshot,
		dhtexchange.OutboundSchedulerConfig{Gates: assembly.config.DHT.Gates, Feed: feeder},
	)

	gateStatus := dhtGateStatusSource{
		snapshot: gateSource.Snapshot,
		config:   assembly.config.DHT.Gates,
	}

	return dhtOutboundProcess{
		cycle: dhtOutboundRosterCycle{
			cycle: scheduler, roster: assembly.roster, events: assembly.events,
		},
		interval:   assembly.config.DHT.Interval,
		gates:      newDHTGateStatusEndpoint(gateStatus),
		gateStatus: gateStatus,
	}
}

func (s dhtGateStateSource) Snapshot(ctx context.Context) dhtexchange.GateState {
	words, err := s.postings.RWICount(ctx)
	localRWIKnown := err == nil
	if err != nil {
		slog.WarnContext(ctx, dhtLocalRWICountUnavailableMessage, slog.Any("error", err))
	}

	atCapacity, err := s.storage.AtCapacity(ctx)
	storageKnown := err == nil
	storageAvailable := err == nil && !atCapacity
	if err != nil {
		slog.WarnContext(ctx, dhtStorageCapacityUnavailableMessage, slog.Any("error", err))
	}

	publicReachable := false
	if s.reachability != nil {
		publicReachable = s.reachability.Reachable(ctx)
	}
	crawlQueueSize, crawlQueueKnown := s.crawl.observation(ctx)
	indexQueueSize, indexQueueKnown := s.index.observation(ctx)

	return dhtexchange.GateState{
		PublicReachable:  publicReachable,
		LocalPeerKnown:   true,
		ConnectedPeers:   len(s.roster.ReachablePeers(ctx)),
		LocalRWIWords:    words,
		LocalRWIKnown:    localRWIKnown,
		CrawlQueueSize:   crawlQueueSize,
		CrawlQueueKnown:  crawlQueueKnown,
		IndexQueueSize:   indexQueueSize,
		IndexQueueKnown:  indexQueueKnown,
		StorageAvailable: storageAvailable,
		StorageKnown:     storageKnown,
	}
}
