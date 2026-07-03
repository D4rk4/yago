package yagonode

import (
	"net/http"

	"github.com/D4rk4/yago/yacynode/internal/documentsearch"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacynode/internal/peerroster"
	"github.com/D4rk4/yago/yacynode/internal/searchcore"
	"github.com/D4rk4/yago/yacynode/internal/searchlocal"
	"github.com/D4rk4/yago/yacynode/internal/searchremote"
	"github.com/D4rk4/yago/yacynode/internal/tavilyapi"
	"github.com/D4rk4/yago/yacynode/internal/yacysearch"
)

type publicSearchAssembly struct {
	storage              nodeStorage
	roster               peerroster.Roster
	identity             nodeidentity.Identity
	dht                  dhtDistributionConfig
	client               *http.Client
	dhtSearchTargetIndex func(int) (int, error)
	searchAPIKey         string
}

func mountNodePublicSearch(
	mux *http.ServeMux,
	assembly publicSearchAssembly,
) {
	local := searchlocal.NewSearcher(assembly.storage.searchIndex)
	if assembly.storage.searchIndex == nil {
		local = documentsearch.NewLocalSearcherWithDocuments(
			assembly.storage.postings,
			assembly.storage.urlDirectory,
			assembly.storage.documentDirectory,
			searchPostingsPerWord,
		)
	}
	remote := searchremote.NewSearcher(searchremote.Config{
		Client:             assembly.client,
		NetworkName:        assembly.identity.NetworkName,
		Peers:              assembly.roster,
		Redundancy:         assembly.dht.Redundancy,
		MinimumPeerAgeDays: assembly.dht.MinimumPeerAgeDays,
		PartitionExponent:  assembly.dht.PartitionExponent,
		RandomTargetIndex:  assembly.dhtSearchTargetIndex,
	})
	search := searchcore.NewFederatedSearcher(local, remote)
	yacysearch.Mount(mux, search)
	tavilyapi.Mount(
		mux,
		search,
		assembly.storage.documentDirectory,
		tavilyapi.SearchAccessPolicy{BearerToken: assembly.searchAPIKey},
	)
	tavilyapi.MountExtract(
		mux,
		assembly.storage.documentDirectory,
		tavilyapi.SearchAccessPolicy{BearerToken: assembly.searchAPIKey},
	)
}
