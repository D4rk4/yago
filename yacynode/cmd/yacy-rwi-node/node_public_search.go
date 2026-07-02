package main

import (
	"net/http"

	"github.com/D4rk4/yago/yacynode/internal/documentsearch"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacynode/internal/peerroster"
	"github.com/D4rk4/yago/yacynode/internal/searchcore"
	"github.com/D4rk4/yago/yacynode/internal/searchremote"
	"github.com/D4rk4/yago/yacynode/internal/yacysearch"
)

type publicSearchAssembly struct {
	storage  nodeStorage
	roster   peerroster.Roster
	identity nodeidentity.Identity
	dht      dhtDistributionConfig
	client   *http.Client
}

func mountNodePublicSearch(
	mux *http.ServeMux,
	assembly publicSearchAssembly,
) {
	local := documentsearch.NewLocalSearcher(
		assembly.storage.postings,
		assembly.storage.urlDirectory,
		searchPostingsPerWord,
	)
	remote := searchremote.NewSearcher(searchremote.Config{
		Client:             assembly.client,
		NetworkName:        assembly.identity.NetworkName,
		Peers:              assembly.roster,
		Redundancy:         assembly.dht.Redundancy,
		MinimumPeerAgeDays: assembly.dht.MinimumPeerAgeDays,
		PartitionExponent:  assembly.dht.PartitionExponent,
	})
	yacysearch.Mount(
		mux,
		searchcore.NewFederatedSearcher(local, remote),
	)
}
