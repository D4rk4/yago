// Package bootstrap fetches the configured YaCy seed lists and returns the seeds
// they advertise. It is the cold-start seed source the peer roster consults when
// it holds no peers yet; once the roster is populated it is no longer needed.
package bootstrap

import (
	"context"
	"net/http"

	"github.com/D4rk4/yago/yacymodel"
)

type SeedSource interface {
	Fetch(ctx context.Context) []yacymodel.Seed
}

type SeedImportObserver interface {
	ObserveSeedlistImport(seedCount int)
}

func New(client *http.Client, urls []string) SeedSource {
	return NewObserved(client, urls, nil)
}

func NewObserved(client *http.Client, urls []string, observer SeedImportObserver) SeedSource {
	return &seedlists{fetcher: newHTTPSeedlistFetcher(client), urls: urls, observer: observer}
}
