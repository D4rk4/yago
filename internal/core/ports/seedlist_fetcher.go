package ports

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type SeedlistFetcher interface {
	Fetch(ctx context.Context, url string) ([]yacymodel.Seed, error)
}
