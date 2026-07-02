package main

import (
	"net/http"

	"github.com/D4rk4/yago/yacynode/internal/documentsearch"
	"github.com/D4rk4/yago/yacynode/internal/yacysearch"
)

func mountNodePublicSearch(mux *http.ServeMux, storage nodeStorage) {
	yacysearch.MountJSON(
		mux,
		documentsearch.NewLocalSearcher(
			storage.postings,
			storage.urlDirectory,
			searchPostingsPerWord,
		),
	)
}
