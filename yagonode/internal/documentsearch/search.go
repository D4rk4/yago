// Package documentsearch finds documents containing wanted terms, orders them by
// relevance, and reports how many documents matched each term.
package documentsearch

import (
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagoproto"
)

// SearchConfig carries the backing stores and limits for the inbound YaCy
// remote-search endpoint.
type SearchConfig struct {
	Index          rwi.PostingIndex
	Documents      urlmeta.URLDirectory
	DocumentStore  documentstore.DocumentDirectory
	AnalyzerSearch searchcore.Searcher
	Evidence       QueryMatchEvidenceAnalyzer
	MatchesPerTerm int
	Gate           *httpguard.IntakeGate
	PotentialPeers PotentialPeerObserver
}

func MountSearch(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	config SearchConfig,
) {
	endpoint := searchEndpoint{
		identity: identity,
		searcher: searcher{
			index:          config.Index,
			documents:      config.Documents,
			matchesPerTerm: config.MatchesPerTerm,
		},
		gate:           config.Gate,
		potentialPeers: config.PotentialPeers,
		analyzerRecall: negotiatedAnalyzerRecallSource{
			searcher:  config.AnalyzerSearch,
			documents: config.Documents,
		},
		evidence: queryMatchEvidenceSource{
			documents: config.DocumentStore,
			analyzer:  config.Evidence,
		},
	}

	httpguard.Mount(
		router,
		yagoproto.PathSearch,
		yagoproto.SearchEndpointMethods,
		yagoproto.ParseSearchRequest,
		endpoint.Serve,
	)
}
