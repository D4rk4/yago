package ports

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type TrustedSeedSource interface {
	Trusted(ctx context.Context) []yacymodel.Seed
}
