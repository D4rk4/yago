package yagonode

import (
	"context"
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/documentsearch"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
	"github.com/D4rk4/yago/yagonode/internal/peerblock"
	"github.com/D4rk4/yago/yagonode/internal/peernews"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
	"github.com/D4rk4/yago/yagonode/internal/transfertally"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type nodePeerWireInput struct {
	ctx         context.Context
	config      nodeConfig
	vault       *vault.Vault
	identity    nodeidentity.Identity
	storage     nodeStorage
	telemetry   nodeTelemetry
	remoteCrawl *remotecrawl.Broker
}

type nodePeerWire struct {
	roster       peerroster.Roster
	news         *peernews.Pool
	tally        *transfertally.Tally
	blocks       *peerblock.Store
	lifecycle    *nodePeerLifecycle
	queues       *selfSeedQueueStatistics
	report       nodestatus.Report
	mux          *http.ServeMux
	router       httpguard.WireRouter
	reachability *peerannouncement.ExternalReachabilityEvidence
}

func assembleNodePeerWire(in nodePeerWireInput) (nodePeerWire, error) {
	roster, news, tally, blocks, err := openPeerStores(
		in.ctx,
		in.vault,
		in.identity.Hash,
		in.telemetry.peer,
	)
	if err != nil {
		return nodePeerWire{}, err
	}
	potentialPeers, _ := roster.(documentsearch.PotentialPeerObserver)
	lifecycle := newNodePeerLifecycle(in.ctx, roster, potentialPeers)
	reachability := peerannouncement.NewExternalReachabilityEvidence()
	var remoteCrawlPending remoteCrawlPendingSource
	if in.remoteCrawl != nil {
		remoteCrawlPending = in.remoteCrawl
	}
	queues := newSelfSeedQueueStatistics(remoteCrawlPending)
	report := newNodeStatusReport(in.identity, nodeStatusSources{
		storage: in.storage, roster: roster, news: news, tally: tally,
		classification: reachability, queues: queues,
	})
	mux, router := mountDHTObservedNodeWire(dhtObservedNodeWireInput{
		config: in.config, identity: in.identity, storage: in.storage,
		peers: roster, telemetry: in.telemetry,
		observation: nodeWireObservation{report: report, tally: tally},
		remoteCrawl: in.remoteCrawl, peerLifecycle: lifecycle,
	})

	return nodePeerWire{
		roster: roster, news: news, tally: tally, blocks: blocks,
		lifecycle: lifecycle, queues: queues, report: report, mux: mux, router: router,
		reachability: reachability,
	}, nil
}
