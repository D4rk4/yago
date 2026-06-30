// Package bootstrap fetches the configured YaCy seed lists and returns the seeds
// they advertise. It is the cold-start seed source the peer roster consults when
// it holds no peers yet; once the roster is populated it is no longer needed.
package bootstrap

import (
	"context"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type SeedSource interface {
	Fetch(ctx context.Context) []yacymodel.Seed
}

func New(client *http.Client, urls []string) SeedSource {
	return &seedlists{fetcher: newHTTPSeedlistFetcher(client), urls: urls}
}
