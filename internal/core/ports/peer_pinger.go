package ports

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type PeerPinger interface {
	Ping(ctx context.Context, peer yacymodel.Seed) error
}
