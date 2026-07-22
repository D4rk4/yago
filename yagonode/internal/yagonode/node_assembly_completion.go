package yagonode

import (
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/peerblock"
	"github.com/D4rk4/yago/yagonode/internal/peernews"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/transfertally"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type nodeAssemblyCompletion struct {
	config        nodeConfig
	telemetry     nodeTelemetry
	identity      nodeidentity.Identity
	storage       nodeStorage
	mux           *http.ServeMux
	exchange      peerExchangeRuntime
	surfaces      nodeSurfaces
	report        nodestatus.Report
	roster        peerroster.Roster
	news          *peernews.Pool
	blocks        *peerblock.Store
	vault         *vault.Vault
	client        *http.Client
	tally         *transfertally.Tally
	peerLifecycle *nodePeerLifecycle
}

func completeNodeAssembly(in nodeAssemblyCompletion) node {
	return newAssembledNode(nodeParts{
		mux:           in.mux,
		publicMux:     in.surfaces.publicMux,
		storage:       in.storage,
		announcer:     in.exchange.announcer,
		lanBeacon:     buildLANBeacon(in.config, in.identity, in.exchange.announcer),
		crawl:         in.surfaces.crawl,
		dht:           in.surfaces.dht,
		report:        in.report,
		searcher:      in.surfaces.searcher,
		suggest:       in.surfaces.suggest,
		explanation:   in.surfaces.explanation,
		roster:        in.roster,
		news:          in.news,
		vault:         in.vault,
		client:        in.client,
		peerBlock:     in.blocks,
		denylist:      in.surfaces.denylist,
		activity:      in.surfaces.activity,
		schedules:     in.surfaces.schedules,
		theme:         in.surfaces.theme,
		identity:      in.identity,
		ranking:       in.surfaces.ranking,
		hostRank:      in.surfaces.hostRank,
		spell:         in.surfaces.spell,
		wordForms:     in.surfaces.wordForms,
		judgments:     in.surfaces.judgments,
		clicks:        in.surfaces.clicks,
		models:        in.surfaces.models,
		safety:        in.surfaces.safety,
		hostTrust:     in.surfaces.trust,
		peerEvents:    in.surfaces.peerEvents,
		peerLifecycle: in.peerLifecycle,
		corpusPass:    in.surfaces.corpusPass,
		swarmMorph:    in.config.SwarmMorphology,
		tally:         in.tally,
		events:        in.telemetry.recorder,
		peerType:      in.exchange.externalReachabilityEvidence,
	}, in.telemetry.toggles)
}
