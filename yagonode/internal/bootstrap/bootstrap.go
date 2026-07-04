// Package bootstrap fetches the configured YaCy seed lists and returns the seeds
// they advertise. It is the cold-start seed source the peer roster consults when
// it holds no peers yet; once the roster is populated it is no longer needed.
package bootstrap

import (
	"context"
	"net/http"

	"github.com/D4rk4/yago/yagomodel"
)

type SeedSource interface {
	Fetch(ctx context.Context) []yagomodel.Seed
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

// SeedlistImporter fetches a single seed list on demand — e.g. an operator's
// "refresh now" action — reusing the same egress-screened client and decoder as
// the cold-start source.
type SeedlistImporter struct {
	fetcher httpSeedlistFetcher
}

// NewSeedlistImporter builds an on-demand importer over the given client.
func NewSeedlistImporter(client *http.Client) *SeedlistImporter {
	return &SeedlistImporter{fetcher: newHTTPSeedlistFetcher(client)}
}

// Import fetches and decodes the seeds advertised by one seed-list URL.
func (i *SeedlistImporter) Import(ctx context.Context, url string) ([]yagomodel.Seed, error) {
	return i.fetcher.Fetch(ctx, url)
}
