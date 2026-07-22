package rwi

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
)

type SenderDirectory interface {
	PeerByHash(ctx context.Context, peer yagomodel.Hash) (yagomodel.Seed, bool)
}
