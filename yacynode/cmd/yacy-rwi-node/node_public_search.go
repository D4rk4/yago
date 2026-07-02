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

func mountNodePublicSearch(
	mux *http.ServeMux,
	storage nodeStorage,
	roster peerroster.Roster,
	identity nodeidentity.Identity,
	client *http.Client,
) {
	local := documentsearch.NewLocalSearcher(
		storage.postings,
		storage.urlDirectory,
		searchPostingsPerWord,
	)
	remote := searchremote.NewSearcher(searchremote.Config{
		Client:      client,
		NetworkName: identity.NetworkName,
		Peers:       roster,
	})
	yacysearch.MountJSON(
		mux,
		searchcore.NewFederatedSearcher(local, remote),
	)
}
